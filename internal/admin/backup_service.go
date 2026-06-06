package admin

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"log/slog"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"sync"
	"time"

	v4 "github.com/aws/aws-sdk-go-v2/aws/signer/v4"
	awsconfig "github.com/aws/aws-sdk-go-v2/config"
	"github.com/aws/aws-sdk-go-v2/credentials"
	"github.com/aws/aws-sdk-go-v2/service/s3"
	"github.com/brevyn/brevyn-cloud/internal/config"
	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/robfig/cron/v3"
)

const (
	settingCloudBackupS3Config = "cloud_backup_s3_config"
	settingCloudBackupSchedule = "cloud_backup_schedule"
	maxBackupRecords           = 200
)

var (
	errBackupNotFound        = errors.New("backup_not_found")
	errBackupInProgress      = errors.New("backup_in_progress")
	errRestoreInProgress     = errors.New("restore_in_progress")
	errBackupNotCompleted    = errors.New("backup_not_completed")
	errBackupS3NotConfigured = errors.New("backup_s3_not_configured")
	errRestoreDisabled       = errors.New("restore_disabled")
)

type BackupService struct {
	cfg      *config.Config
	postgres *pgxpool.Pool
	gateway  *GatewaySettingsService

	opMu      sync.Mutex
	backingUp bool
	restoring bool

	cronMu      sync.Mutex
	cronSched   *cron.Cron
	cronEntryID cron.EntryID

	bgCtx    context.Context
	bgCancel context.CancelFunc
	wg       sync.WaitGroup
}

type BackupS3Config struct {
	Endpoint          string `json:"endpoint"`
	Region            string `json:"region"`
	Bucket            string `json:"bucket"`
	AccessKeyID       string `json:"accessKeyId"`
	SecretAccessKey   string `json:"secretAccessKey,omitempty"`
	Prefix            string `json:"prefix"`
	ForcePathStyle    bool   `json:"forcePathStyle"`
	SecretConfigured  bool   `json:"secretConfigured"`
	StorageConfigured bool   `json:"storageConfigured"`
}

type BackupScheduleConfig struct {
	Enabled     bool   `json:"enabled"`
	CronExpr    string `json:"cronExpr"`
	RetainDays  int    `json:"retainDays"`
	RetainCount int    `json:"retainCount"`
}

type BackupRuntimeConfig struct {
	BackupDir          string `json:"backupDir"`
	RetentionDays      int    `json:"retentionDays"`
	RestoreEnabled     bool   `json:"restoreEnabled"`
	S3Configured       bool   `json:"s3Configured"`
	ScheduleEnabled    bool   `json:"scheduleEnabled"`
	ScheduleCronExpr   string `json:"scheduleCronExpr"`
	PostgresClientNote string `json:"postgresClientNote"`
}

type BackupRecord struct {
	ID            string     `json:"id"`
	Status        string     `json:"status"`
	Progress      string     `json:"progress"`
	BackupType    string     `json:"backupType"`
	StorageKind   string     `json:"storageKind"`
	FileName      string     `json:"fileName"`
	FilePath      string     `json:"filePath"`
	S3Key         string     `json:"s3Key"`
	SizeBytes     int64      `json:"sizeBytes"`
	SHA256        string     `json:"sha256"`
	TriggeredBy   string     `json:"triggeredBy"`
	ErrorMessage  string     `json:"errorMessage"`
	StartedAt     time.Time  `json:"startedAt"`
	FinishedAt    *time.Time `json:"finishedAt"`
	ExpiresAt     *time.Time `json:"expiresAt"`
	RestoreStatus string     `json:"restoreStatus"`
	RestoreError  string     `json:"restoreError"`
	RestoredAt    *time.Time `json:"restoredAt"`
	CreatedAt     time.Time  `json:"createdAt"`
	UpdatedAt     time.Time  `json:"updatedAt"`
}

type backupObjectStore struct {
	client *s3.Client
	bucket string
}

func NewBackupService(cfg *config.Config, postgres *pgxpool.Pool, gateway *GatewaySettingsService) *BackupService {
	bgCtx, bgCancel := context.WithCancel(context.Background())
	return &BackupService{
		cfg:      cfg,
		postgres: postgres,
		gateway:  gateway,
		bgCtx:    bgCtx,
		bgCancel: bgCancel,
	}
}

func (s *BackupService) Start() {
	s.recoverStaleRecords()
	parser := cron.NewParser(cron.Minute | cron.Hour | cron.Dom | cron.Month | cron.Dow | cron.Descriptor)
	s.cronSched = cron.New(cron.WithParser(parser))
	s.cronSched.Start()

	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	schedule, err := s.GetSchedule(ctx)
	if err != nil {
		slog.Warn("load cloud backup schedule failed", "error", err)
		return
	}
	if schedule.Enabled && strings.TrimSpace(schedule.CronExpr) != "" {
		if err := s.applySchedule(schedule); err != nil {
			slog.Warn("apply cloud backup schedule failed", "error", err)
		}
	}
}

func (s *BackupService) Stop() {
	if s.bgCancel != nil {
		s.bgCancel()
	}
	s.cronMu.Lock()
	if s.cronSched != nil {
		stopCtx := s.cronSched.Stop()
		select {
		case <-stopCtx.Done():
		case <-time.After(5 * time.Second):
		}
	}
	s.cronMu.Unlock()

	done := make(chan struct{})
	go func() {
		s.wg.Wait()
		close(done)
	}()
	select {
	case <-done:
	case <-time.After(30 * time.Second):
	}
}

func (s *BackupService) RuntimeConfig(ctx context.Context) (BackupRuntimeConfig, error) {
	s3Config, err := s.GetS3Config(ctx)
	if err != nil {
		return BackupRuntimeConfig{}, err
	}
	schedule, err := s.GetSchedule(ctx)
	if err != nil {
		return BackupRuntimeConfig{}, err
	}
	return BackupRuntimeConfig{
		BackupDir:          s.cfg.BackupDir,
		RetentionDays:      s.cfg.BackupRetentionDays,
		RestoreEnabled:     s.cfg.AllowAdminDBRestore,
		S3Configured:       s3Config.StorageConfigured,
		ScheduleEnabled:    schedule.Enabled,
		ScheduleCronExpr:   schedule.CronExpr,
		PostgresClientNote: "requires pg_dump and pg_restore in the API container",
	}, nil
}

func (s *BackupService) GetS3Config(ctx context.Context) (BackupS3Config, error) {
	cfg, err := s.loadS3Config(ctx)
	if err != nil {
		return BackupS3Config{}, err
	}
	cfg.SecretConfigured = strings.TrimSpace(cfg.SecretAccessKey) != ""
	cfg.StorageConfigured = cfg.isConfigured()
	cfg.SecretAccessKey = ""
	return cfg, nil
}

func (s *BackupService) UpdateS3Config(ctx context.Context, cfg BackupS3Config, adminID int64) (BackupS3Config, error) {
	cfg.normalize()
	cfg.SecretConfigured = false
	cfg.StorageConfigured = false
	if strings.TrimSpace(cfg.SecretAccessKey) == "" {
		if raw, ok, err := s.getAppSetting(ctx, settingCloudBackupS3Config); err != nil {
			return BackupS3Config{}, err
		} else if ok {
			var stored BackupS3Config
			if err := json.Unmarshal([]byte(raw), &stored); err == nil {
				cfg.SecretAccessKey = stored.SecretAccessKey
			}
		}
	} else {
		encrypted, err := s.gateway.EncryptSecret(cfg.SecretAccessKey)
		if err != nil {
			return BackupS3Config{}, fmt.Errorf("encrypt_s3_secret: %w", err)
		}
		cfg.SecretAccessKey = encrypted
	}

	data, err := json.Marshal(cfg)
	if err != nil {
		return BackupS3Config{}, err
	}
	if err := s.upsertAppSetting(ctx, settingCloudBackupS3Config, string(data), true, adminID); err != nil {
		return BackupS3Config{}, err
	}
	return s.GetS3Config(ctx)
}

func (s *BackupService) TestS3Connection(ctx context.Context, cfg BackupS3Config) error {
	cfg.normalize()
	if strings.TrimSpace(cfg.SecretAccessKey) == "" {
		stored, err := s.loadS3Config(ctx)
		if err != nil {
			return err
		}
		cfg.SecretAccessKey = stored.SecretAccessKey
	}
	if !cfg.isConfigured() {
		return errBackupS3NotConfigured
	}
	store, err := newBackupObjectStore(ctx, cfg)
	if err != nil {
		return err
	}
	return store.HeadBucket(ctx)
}

func (s *BackupService) GetSchedule(ctx context.Context) (BackupScheduleConfig, error) {
	raw, ok, err := s.getAppSetting(ctx, settingCloudBackupSchedule)
	if err != nil {
		return BackupScheduleConfig{}, err
	}
	if !ok || strings.TrimSpace(raw) == "" {
		return BackupScheduleConfig{
			CronExpr:   "0 3 * * *",
			RetainDays: s.cfg.BackupRetentionDays,
		}, nil
	}
	var cfg BackupScheduleConfig
	if err := json.Unmarshal([]byte(raw), &cfg); err != nil {
		return BackupScheduleConfig{}, fmt.Errorf("backup_schedule_corrupt: %w", err)
	}
	if cfg.RetainDays < 0 {
		cfg.RetainDays = 0
	}
	if cfg.RetainCount < 0 {
		cfg.RetainCount = 0
	}
	return cfg, nil
}

func (s *BackupService) UpdateSchedule(ctx context.Context, cfg BackupScheduleConfig, adminID int64) (BackupScheduleConfig, error) {
	cfg.CronExpr = strings.TrimSpace(cfg.CronExpr)
	if cfg.Enabled && cfg.CronExpr == "" {
		return BackupScheduleConfig{}, fmt.Errorf("cron_required")
	}
	if cfg.CronExpr != "" {
		parser := cron.NewParser(cron.Minute | cron.Hour | cron.Dom | cron.Month | cron.Dow | cron.Descriptor)
		if _, err := parser.Parse(cfg.CronExpr); err != nil {
			return BackupScheduleConfig{}, fmt.Errorf("invalid_cron: %w", err)
		}
	}
	if cfg.RetainDays < 0 {
		cfg.RetainDays = 0
	}
	if cfg.RetainCount < 0 {
		cfg.RetainCount = 0
	}

	data, err := json.Marshal(cfg)
	if err != nil {
		return BackupScheduleConfig{}, err
	}
	if err := s.upsertAppSetting(ctx, settingCloudBackupSchedule, string(data), false, adminID); err != nil {
		return BackupScheduleConfig{}, err
	}
	if cfg.Enabled {
		if err := s.applySchedule(cfg); err != nil {
			return BackupScheduleConfig{}, err
		}
	} else {
		s.removeSchedule()
	}
	return cfg, nil
}

func (s *BackupService) StartBackup(ctx context.Context, triggeredBy string, expireDays int) (BackupRecord, error) {
	if err := s.acquireBackupOperation(); err != nil {
		return BackupRecord{}, err
	}
	record, err := s.createBackupRecord(ctx, triggeredBy, expireDays)
	if err != nil {
		s.releaseBackupOperation()
		return BackupRecord{}, err
	}

	s.wg.Add(1)
	go func() {
		defer s.wg.Done()
		defer s.releaseBackupOperation()
		s.runBackupRecord(s.bgCtx, record)
	}()
	return record, nil
}

func (s *BackupService) ListBackups(ctx context.Context, limit int) ([]BackupRecord, int64, error) {
	if limit <= 0 || limit > maxBackupRecords {
		limit = 100
	}
	rows, err := s.postgres.Query(ctx, `
		SELECT id, public_id, status, progress, backup_type, storage_kind, file_name, file_path,
			s3_key, size_bytes, sha256, triggered_by, error_message, started_at, finished_at,
			expires_at, restore_status, restore_error, restored_at, created_at, updated_at
		FROM backup_records
		ORDER BY created_at DESC, id DESC
		LIMIT $1
	`, limit)
	if err != nil {
		return nil, 0, err
	}
	defer rows.Close()
	items := make([]BackupRecord, 0, limit)
	for rows.Next() {
		item, err := scanBackupRecord(rows)
		if err != nil {
			return nil, 0, err
		}
		items = append(items, item)
	}
	if err := rows.Err(); err != nil {
		return nil, 0, err
	}

	var total int64
	if err := s.postgres.QueryRow(ctx, `SELECT count(*) FROM backup_records`).Scan(&total); err != nil {
		return nil, 0, err
	}
	return items, total, nil
}

func (s *BackupService) GetBackup(ctx context.Context, publicID string) (BackupRecord, error) {
	record, err := s.getBackup(ctx, publicID)
	if errors.Is(err, pgx.ErrNoRows) {
		return BackupRecord{}, errBackupNotFound
	}
	return record, err
}

func (s *BackupService) DeleteBackup(ctx context.Context, publicID string) error {
	record, err := s.GetBackup(ctx, publicID)
	if err != nil {
		return err
	}
	if record.Status == "running" || record.Status == "pending" || record.RestoreStatus == "running" {
		return errBackupInProgress
	}
	if record.FilePath != "" && s.isPathInsideBackupDir(record.FilePath) {
		_ = os.Remove(record.FilePath)
		_ = os.Remove(record.FilePath + ".sha256")
	}
	if strings.TrimSpace(record.S3Key) != "" {
		if store, err := s.objectStoreFromSavedConfig(ctx); err == nil {
			_ = store.Delete(ctx, record.S3Key)
		}
	}
	_, err = s.postgres.Exec(ctx, `DELETE FROM backup_records WHERE public_id = $1`, publicID)
	return err
}

func (s *BackupService) DownloadURL(ctx context.Context, publicID string) (string, error) {
	record, err := s.GetBackup(ctx, publicID)
	if err != nil {
		return "", err
	}
	if record.Status != "completed" {
		return "", errBackupNotCompleted
	}
	if strings.TrimSpace(record.S3Key) == "" {
		return "", errBackupS3NotConfigured
	}
	store, err := s.objectStoreFromSavedConfig(ctx)
	if err != nil {
		return "", err
	}
	return store.PresignURL(ctx, record.S3Key, time.Hour)
}

func (s *BackupService) LocalFileForDownload(ctx context.Context, publicID string) (BackupRecord, string, error) {
	record, err := s.GetBackup(ctx, publicID)
	if err != nil {
		return BackupRecord{}, "", err
	}
	if record.Status != "completed" {
		return BackupRecord{}, "", errBackupNotCompleted
	}
	if strings.TrimSpace(record.FilePath) == "" || !s.isPathInsideBackupDir(record.FilePath) {
		return BackupRecord{}, "", errBackupNotFound
	}
	if _, err := os.Stat(record.FilePath); err != nil {
		return BackupRecord{}, "", err
	}
	return record, record.FilePath, nil
}

func (s *BackupService) StartRestore(ctx context.Context, publicID string) (BackupRecord, error) {
	if !s.cfg.AllowAdminDBRestore {
		return BackupRecord{}, errRestoreDisabled
	}
	if err := s.acquireRestoreOperation(); err != nil {
		return BackupRecord{}, err
	}
	record, err := s.GetBackup(ctx, publicID)
	if err != nil {
		s.releaseRestoreOperation()
		return BackupRecord{}, err
	}
	if record.Status != "completed" {
		s.releaseRestoreOperation()
		return BackupRecord{}, errBackupNotCompleted
	}

	record.RestoreStatus = "running"
	record.RestoreError = ""
	if err := s.updateRestoreState(ctx, record); err != nil {
		s.releaseRestoreOperation()
		return BackupRecord{}, err
	}

	s.wg.Add(1)
	go func() {
		defer s.wg.Done()
		defer s.releaseRestoreOperation()
		s.runRestoreRecord(s.bgCtx, record)
	}()
	return record, nil
}

func (s *BackupService) createBackupRecord(ctx context.Context, triggeredBy string, expireDays int) (BackupRecord, error) {
	now := time.Now().UTC()
	if expireDays < 0 {
		expireDays = s.cfg.BackupRetentionDays
	}
	var expiresAt *time.Time
	if expireDays > 0 {
		value := now.AddDate(0, 0, expireDays)
		expiresAt = &value
	}
	fileName := fmt.Sprintf("brevyn-cloud-%s.dump", now.Format("20060102T150405Z"))
	if strings.TrimSpace(triggeredBy) == "" {
		triggeredBy = "manual"
	}
	record := BackupRecord{
		ID:          backupPublicID(),
		Status:      "running",
		Progress:    "pending",
		BackupType:  "postgres",
		StorageKind: "local",
		FileName:    fileName,
		TriggeredBy: triggeredBy,
		StartedAt:   now,
		ExpiresAt:   expiresAt,
		CreatedAt:   now,
		UpdatedAt:   now,
	}
	if err := s.insertBackupRecord(ctx, record); err != nil {
		return BackupRecord{}, err
	}
	return record, nil
}

func (s *BackupService) runBackupRecord(ctx context.Context, record BackupRecord) {
	runCtx, cancel := context.WithTimeout(ctx, 30*time.Minute)
	defer cancel()

	if err := s.executeBackup(runCtx, &record); err != nil {
		now := time.Now().UTC()
		record.Status = "failed"
		record.Progress = ""
		record.ErrorMessage = err.Error()
		record.FinishedAt = &now
		_ = s.updateBackupRecord(context.Background(), record)
		return
	}
	now := time.Now().UTC()
	record.Status = "completed"
	record.Progress = ""
	record.ErrorMessage = ""
	record.FinishedAt = &now
	if err := s.updateBackupRecord(context.Background(), record); err != nil {
		slog.Warn("update cloud backup record failed", "backup", record.ID, "error", err)
	}

	schedule, err := s.GetSchedule(context.Background())
	if err == nil {
		_ = s.cleanupOldBackups(context.Background(), schedule)
	}
}

func (s *BackupService) executeBackup(ctx context.Context, record *BackupRecord) error {
	backupDir := strings.TrimSpace(s.cfg.BackupDir)
	if backupDir == "" {
		return fmt.Errorf("backup_dir_required")
	}
	if err := os.MkdirAll(backupDir, 0o700); err != nil {
		return fmt.Errorf("create_backup_dir: %w", err)
	}
	filePath := filepath.Join(backupDir, record.FileName)
	record.FilePath = filePath
	record.Progress = "dumping"
	if err := s.updateBackupRecord(context.Background(), *record); err != nil {
		return err
	}

	if err := s.runPgDump(ctx, filePath); err != nil {
		_ = os.Remove(filePath)
		return err
	}
	size, sha, err := fileDigest(filePath)
	if err != nil {
		return err
	}
	record.SizeBytes = size
	record.SHA256 = sha
	if err := os.WriteFile(filePath+".sha256", []byte(sha+"  "+record.FileName+"\n"), 0o600); err != nil {
		return fmt.Errorf("write_sha256: %w", err)
	}

	s3Config, err := s.loadS3Config(ctx)
	if err != nil {
		return err
	}
	if s3Config.isConfigured() {
		record.Progress = "uploading"
		if err := s.updateBackupRecord(context.Background(), *record); err != nil {
			return err
		}
		store, err := newBackupObjectStore(ctx, s3Config)
		if err != nil {
			return err
		}
		s3Key := buildBackupS3Key(s3Config, record.FileName)
		if err := store.UploadFile(ctx, s3Key, filePath, "application/octet-stream"); err != nil {
			return err
		}
		record.S3Key = s3Key
		record.StorageKind = "local+s3"
	}
	return nil
}

func (s *BackupService) runRestoreRecord(ctx context.Context, record BackupRecord) {
	runCtx, cancel := context.WithTimeout(ctx, 45*time.Minute)
	defer cancel()

	preRestoreRecord, err := s.createAndRunPreRestoreBackup(runCtx, record.ID)
	if err != nil {
		record.RestoreStatus = "failed"
		record.RestoreError = "pre_restore_backup_failed: " + err.Error()
		_ = s.updateRestoreState(context.Background(), record)
		return
	}

	sourcePath, cleanup, err := s.restoreSourceFile(runCtx, record)
	if cleanup != nil {
		defer cleanup()
	}
	if err != nil {
		record.RestoreStatus = "failed"
		record.RestoreError = err.Error()
		_ = s.updateRestoreState(context.Background(), record)
		return
	}
	if err := s.runPgRestore(runCtx, sourcePath); err != nil {
		s.postgres.Reset()
		now := time.Now().UTC()
		record.RestoreStatus = "failed"
		record.RestoreError = err.Error()
		record.UpdatedAt = now
		preRestoreRecord.UpdatedAt = now
		s.persistRestoreRecords(context.Background(), record, preRestoreRecord)
		return
	}

	// pg_restore rewrites tables under the API; reset cached connections before
	// writing post-restore metadata back into the restored database.
	s.postgres.Reset()

	now := time.Now().UTC()
	record.Status = "completed"
	record.Progress = ""
	record.ErrorMessage = ""
	record.RestoreStatus = "completed"
	record.RestoreError = ""
	record.RestoredAt = &now
	record.UpdatedAt = now
	preRestoreRecord.UpdatedAt = now
	s.persistRestoreRecords(context.Background(), record, preRestoreRecord)
}

func (s *BackupService) createAndRunPreRestoreBackup(ctx context.Context, restoringBackupID string) (BackupRecord, error) {
	record, err := s.createBackupRecord(ctx, "pre_restore:"+restoringBackupID, s.cfg.BackupRetentionDays)
	if err != nil {
		return BackupRecord{}, err
	}
	if err := s.executeBackup(ctx, &record); err != nil {
		now := time.Now().UTC()
		record.Status = "failed"
		record.Progress = ""
		record.ErrorMessage = err.Error()
		record.FinishedAt = &now
		_ = s.updateBackupRecord(context.Background(), record)
		return record, err
	}
	now := time.Now().UTC()
	record.Status = "completed"
	record.Progress = ""
	record.FinishedAt = &now
	record.UpdatedAt = now
	return record, s.updateBackupRecord(context.Background(), record)
}

func (s *BackupService) persistRestoreRecords(ctx context.Context, record BackupRecord, preRestoreRecord BackupRecord) {
	if err := s.upsertBackupRecord(ctx, record); err != nil {
		slog.Warn("upsert restored cloud backup record failed", "backup", record.ID, "error", err)
	}
	if strings.TrimSpace(preRestoreRecord.ID) == "" {
		return
	}
	if err := s.upsertBackupRecord(ctx, preRestoreRecord); err != nil {
		slog.Warn("upsert pre-restore cloud backup record failed", "backup", preRestoreRecord.ID, "error", err)
	}
}

func (s *BackupService) restoreSourceFile(ctx context.Context, record BackupRecord) (string, func(), error) {
	if strings.TrimSpace(record.FilePath) != "" && s.isPathInsideBackupDir(record.FilePath) {
		if _, err := os.Stat(record.FilePath); err == nil {
			return record.FilePath, nil, nil
		}
	}
	if strings.TrimSpace(record.S3Key) == "" {
		return "", nil, fmt.Errorf("backup_file_missing")
	}
	if err := os.MkdirAll(s.cfg.BackupDir, 0o700); err != nil {
		return "", nil, err
	}
	store, err := s.objectStoreFromSavedConfig(ctx)
	if err != nil {
		return "", nil, err
	}
	tempPath := filepath.Join(s.cfg.BackupDir, "restore-"+record.ID+".dump")
	if err := store.DownloadFile(ctx, record.S3Key, tempPath); err != nil {
		return "", nil, err
	}
	return tempPath, func() { _ = os.Remove(tempPath) }, nil
}

func (s *BackupService) runPgDump(ctx context.Context, filePath string) error {
	if _, err := exec.LookPath("pg_dump"); err != nil {
		return fmt.Errorf("pg_dump_not_found")
	}
	cmd := exec.CommandContext(ctx, "pg_dump",
		"--format=custom",
		"--no-owner",
		"--no-acl",
		"--file", filePath,
		s.cfg.DatabaseURL,
	)
	cmd.Env = append(os.Environ(), "PGCONNECT_TIMEOUT=10")
	output, err := cmd.CombinedOutput()
	if err != nil {
		return fmt.Errorf("pg_dump_failed: %v: %s", err, strings.TrimSpace(string(output)))
	}
	return nil
}

func (s *BackupService) runPgRestore(ctx context.Context, filePath string) error {
	if _, err := exec.LookPath("pg_restore"); err != nil {
		return fmt.Errorf("pg_restore_not_found")
	}
	cmd := exec.CommandContext(ctx, "pg_restore",
		"--clean",
		"--if-exists",
		"--no-owner",
		"--no-acl",
		"--dbname", s.cfg.DatabaseURL,
		filePath,
	)
	cmd.Env = append(os.Environ(), "PGCONNECT_TIMEOUT=10")
	output, err := cmd.CombinedOutput()
	if err != nil {
		return fmt.Errorf("pg_restore_failed: %v: %s", err, strings.TrimSpace(string(output)))
	}
	return nil
}

func (s *BackupService) acquireBackupOperation() error {
	s.opMu.Lock()
	defer s.opMu.Unlock()
	if s.backingUp {
		return errBackupInProgress
	}
	if s.restoring {
		return errRestoreInProgress
	}
	s.backingUp = true
	return nil
}

func (s *BackupService) releaseBackupOperation() {
	s.opMu.Lock()
	s.backingUp = false
	s.opMu.Unlock()
}

func (s *BackupService) acquireRestoreOperation() error {
	s.opMu.Lock()
	defer s.opMu.Unlock()
	if s.restoring {
		return errRestoreInProgress
	}
	if s.backingUp {
		return errBackupInProgress
	}
	s.restoring = true
	return nil
}

func (s *BackupService) releaseRestoreOperation() {
	s.opMu.Lock()
	s.restoring = false
	s.opMu.Unlock()
}

func (s *BackupService) applySchedule(cfg BackupScheduleConfig) error {
	s.cronMu.Lock()
	defer s.cronMu.Unlock()
	if s.cronSched == nil {
		return fmt.Errorf("backup_scheduler_not_started")
	}
	if s.cronEntryID != 0 {
		s.cronSched.Remove(s.cronEntryID)
		s.cronEntryID = 0
	}
	entryID, err := s.cronSched.AddFunc(cfg.CronExpr, func() {
		_, err := s.StartBackup(context.Background(), "scheduled", cfg.RetainDays)
		if err != nil {
			slog.Warn("scheduled cloud backup skipped", "error", err)
		}
	})
	if err != nil {
		return err
	}
	s.cronEntryID = entryID
	return nil
}

func (s *BackupService) removeSchedule() {
	s.cronMu.Lock()
	defer s.cronMu.Unlock()
	if s.cronSched != nil && s.cronEntryID != 0 {
		s.cronSched.Remove(s.cronEntryID)
		s.cronEntryID = 0
	}
}

func (s *BackupService) cleanupOldBackups(ctx context.Context, schedule BackupScheduleConfig) error {
	rows, err := s.postgres.Query(ctx, `
		SELECT id, public_id, status, progress, backup_type, storage_kind, file_name, file_path,
			s3_key, size_bytes, sha256, triggered_by, error_message, started_at, finished_at,
			expires_at, restore_status, restore_error, restored_at, created_at, updated_at
		FROM backup_records
		WHERE status = 'completed'
		ORDER BY created_at DESC, id DESC
		LIMIT $1
	`, maxBackupRecords)
	if err != nil {
		return err
	}
	defer rows.Close()
	records := make([]BackupRecord, 0)
	for rows.Next() {
		record, err := scanBackupRecord(rows)
		if err != nil {
			return err
		}
		records = append(records, record)
	}
	if err := rows.Err(); err != nil {
		return err
	}
	now := time.Now().UTC()
	for index, record := range records {
		shouldDelete := false
		if schedule.RetainCount > 0 && index >= schedule.RetainCount {
			shouldDelete = true
		}
		if schedule.RetainDays > 0 && record.CreatedAt.Before(now.AddDate(0, 0, -schedule.RetainDays)) {
			shouldDelete = true
		}
		if shouldDelete {
			_ = s.DeleteBackup(ctx, record.ID)
		}
	}
	return nil
}

func (s *BackupService) recoverStaleRecords() {
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	now := time.Now().UTC()
	_, _ = s.postgres.Exec(ctx, `
		UPDATE backup_records
		SET status = 'failed',
			progress = '',
			error_message = 'interrupted by server restart',
			finished_at = coalesce(finished_at, $1),
			updated_at = now()
		WHERE status IN ('pending', 'running')
	`, now)
	_, _ = s.postgres.Exec(ctx, `
		UPDATE backup_records
		SET restore_status = 'failed',
			restore_error = 'interrupted by server restart',
			updated_at = now()
		WHERE restore_status = 'running'
	`)
}

func (s *BackupService) insertBackupRecord(ctx context.Context, record BackupRecord) error {
	_, err := s.postgres.Exec(ctx, `
		INSERT INTO backup_records (
			public_id, status, progress, backup_type, storage_kind, file_name, file_path,
			s3_key, size_bytes, sha256, triggered_by, error_message, started_at,
			finished_at, expires_at, restore_status, restore_error, restored_at
		)
		VALUES ($1, $2, $3, $4, $5, $6, $7, $8, $9, $10, $11, $12, $13, $14, $15, $16, $17, $18)
	`, record.ID, record.Status, record.Progress, record.BackupType, record.StorageKind, record.FileName,
		record.FilePath, record.S3Key, record.SizeBytes, record.SHA256, record.TriggeredBy, record.ErrorMessage,
		record.StartedAt, record.FinishedAt, record.ExpiresAt, record.RestoreStatus, record.RestoreError, record.RestoredAt)
	return err
}

func (s *BackupService) upsertBackupRecord(ctx context.Context, record BackupRecord) error {
	if record.CreatedAt.IsZero() {
		record.CreatedAt = time.Now().UTC()
	}
	if record.UpdatedAt.IsZero() {
		record.UpdatedAt = record.CreatedAt
	}
	_, err := s.postgres.Exec(ctx, `
		INSERT INTO backup_records (
			public_id, status, progress, backup_type, storage_kind, file_name, file_path,
			s3_key, size_bytes, sha256, triggered_by, error_message, started_at,
			finished_at, expires_at, restore_status, restore_error, restored_at, created_at, updated_at
		)
		VALUES ($1, $2, $3, $4, $5, $6, $7, $8, $9, $10, $11, $12, $13, $14, $15, $16, $17, $18, $19, $20)
		ON CONFLICT (public_id) DO UPDATE
		SET status = excluded.status,
			progress = excluded.progress,
			backup_type = excluded.backup_type,
			storage_kind = excluded.storage_kind,
			file_name = excluded.file_name,
			file_path = excluded.file_path,
			s3_key = excluded.s3_key,
			size_bytes = excluded.size_bytes,
			sha256 = excluded.sha256,
			triggered_by = excluded.triggered_by,
			error_message = excluded.error_message,
			started_at = excluded.started_at,
			finished_at = excluded.finished_at,
			expires_at = excluded.expires_at,
			restore_status = excluded.restore_status,
			restore_error = excluded.restore_error,
			restored_at = excluded.restored_at,
			updated_at = excluded.updated_at
	`, record.ID, record.Status, record.Progress, record.BackupType, record.StorageKind, record.FileName,
		record.FilePath, record.S3Key, record.SizeBytes, record.SHA256, record.TriggeredBy, record.ErrorMessage,
		record.StartedAt, record.FinishedAt, record.ExpiresAt, record.RestoreStatus, record.RestoreError,
		record.RestoredAt, record.CreatedAt, record.UpdatedAt)
	return err
}

func (s *BackupService) updateBackupRecord(ctx context.Context, record BackupRecord) error {
	_, err := s.postgres.Exec(ctx, `
		UPDATE backup_records
		SET status = $2,
			progress = $3,
			storage_kind = $4,
			file_path = $5,
			s3_key = $6,
			size_bytes = $7,
			sha256 = $8,
			error_message = $9,
			finished_at = $10,
			updated_at = now()
		WHERE public_id = $1
	`, record.ID, record.Status, record.Progress, record.StorageKind, record.FilePath, record.S3Key,
		record.SizeBytes, record.SHA256, record.ErrorMessage, record.FinishedAt)
	return err
}

func (s *BackupService) updateRestoreState(ctx context.Context, record BackupRecord) error {
	_, err := s.postgres.Exec(ctx, `
		UPDATE backup_records
		SET restore_status = $2,
			restore_error = $3,
			restored_at = $4,
			updated_at = now()
		WHERE public_id = $1
	`, record.ID, record.RestoreStatus, record.RestoreError, record.RestoredAt)
	return err
}

func (s *BackupService) getBackup(ctx context.Context, publicID string) (BackupRecord, error) {
	row := s.postgres.QueryRow(ctx, `
		SELECT id, public_id, status, progress, backup_type, storage_kind, file_name, file_path,
			s3_key, size_bytes, sha256, triggered_by, error_message, started_at, finished_at,
			expires_at, restore_status, restore_error, restored_at, created_at, updated_at
		FROM backup_records
		WHERE public_id = $1
	`, strings.TrimSpace(publicID))
	return scanBackupRecord(row)
}

func scanBackupRecord(row pgx.Row) (BackupRecord, error) {
	var internalID int64
	var record BackupRecord
	err := row.Scan(
		&internalID,
		&record.ID,
		&record.Status,
		&record.Progress,
		&record.BackupType,
		&record.StorageKind,
		&record.FileName,
		&record.FilePath,
		&record.S3Key,
		&record.SizeBytes,
		&record.SHA256,
		&record.TriggeredBy,
		&record.ErrorMessage,
		&record.StartedAt,
		&record.FinishedAt,
		&record.ExpiresAt,
		&record.RestoreStatus,
		&record.RestoreError,
		&record.RestoredAt,
		&record.CreatedAt,
		&record.UpdatedAt,
	)
	return record, err
}

func (s *BackupService) loadS3Config(ctx context.Context) (BackupS3Config, error) {
	raw, ok, err := s.getAppSetting(ctx, settingCloudBackupS3Config)
	if err != nil {
		return BackupS3Config{}, err
	}
	if !ok || strings.TrimSpace(raw) == "" {
		return BackupS3Config{Region: "auto", Prefix: "cloud-backups"}, nil
	}
	var cfg BackupS3Config
	if err := json.Unmarshal([]byte(raw), &cfg); err != nil {
		return BackupS3Config{}, fmt.Errorf("backup_s3_config_corrupt: %w", err)
	}
	cfg.normalize()
	if strings.TrimSpace(cfg.SecretAccessKey) != "" {
		secret, err := s.gateway.DecryptSecret(cfg.SecretAccessKey)
		if err != nil {
			return BackupS3Config{}, err
		}
		cfg.SecretAccessKey = secret
	}
	return cfg, nil
}

func (s *BackupService) objectStoreFromSavedConfig(ctx context.Context) (*backupObjectStore, error) {
	cfg, err := s.loadS3Config(ctx)
	if err != nil {
		return nil, err
	}
	if !cfg.isConfigured() {
		return nil, errBackupS3NotConfigured
	}
	return newBackupObjectStore(ctx, cfg)
}

func (s *BackupService) getAppSetting(ctx context.Context, key string) (string, bool, error) {
	var value string
	err := s.postgres.QueryRow(ctx, `SELECT value FROM app_settings WHERE key = $1`, key).Scan(&value)
	if errors.Is(err, pgx.ErrNoRows) {
		return "", false, nil
	}
	if err != nil {
		return "", false, err
	}
	return value, true, nil
}

func (s *BackupService) upsertAppSetting(ctx context.Context, key, value string, sensitive bool, adminID int64) error {
	_, err := s.postgres.Exec(ctx, `
		INSERT INTO app_settings (key, value, sensitive, updated_by_admin_id)
		VALUES ($1, $2, $3, $4)
		ON CONFLICT (key) DO UPDATE
		SET value = excluded.value,
			sensitive = excluded.sensitive,
			updated_by_admin_id = excluded.updated_by_admin_id,
			updated_at = now()
	`, key, value, sensitive, adminID)
	return err
}

func (s *BackupService) isPathInsideBackupDir(path string) bool {
	if strings.TrimSpace(path) == "" || strings.TrimSpace(s.cfg.BackupDir) == "" {
		return false
	}
	base, err := filepath.Abs(s.cfg.BackupDir)
	if err != nil {
		return false
	}
	target, err := filepath.Abs(path)
	if err != nil {
		return false
	}
	rel, err := filepath.Rel(base, target)
	return err == nil && rel != "." && !strings.HasPrefix(rel, ".."+string(filepath.Separator)) && rel != ".."
}

func (c *BackupS3Config) normalize() {
	c.Endpoint = strings.TrimRight(strings.TrimSpace(c.Endpoint), "/")
	c.Region = strings.TrimSpace(c.Region)
	if c.Region == "" {
		c.Region = "auto"
	}
	c.Bucket = strings.TrimSpace(c.Bucket)
	c.AccessKeyID = strings.TrimSpace(c.AccessKeyID)
	c.SecretAccessKey = strings.TrimSpace(c.SecretAccessKey)
	c.Prefix = strings.Trim(strings.TrimSpace(c.Prefix), "/")
	if c.Prefix == "" {
		c.Prefix = "cloud-backups"
	}
}

func (c BackupS3Config) isConfigured() bool {
	return strings.TrimSpace(c.Bucket) != "" &&
		strings.TrimSpace(c.AccessKeyID) != "" &&
		strings.TrimSpace(c.SecretAccessKey) != ""
}

func newBackupObjectStore(ctx context.Context, cfg BackupS3Config) (*backupObjectStore, error) {
	cfg.normalize()
	awsCfg, err := awsconfig.LoadDefaultConfig(ctx,
		awsconfig.WithRegion(cfg.Region),
		awsconfig.WithCredentialsProvider(credentials.NewStaticCredentialsProvider(cfg.AccessKeyID, cfg.SecretAccessKey, "")),
	)
	if err != nil {
		return nil, fmt.Errorf("load_aws_config: %w", err)
	}
	client := s3.NewFromConfig(awsCfg, func(o *s3.Options) {
		if cfg.Endpoint != "" {
			o.BaseEndpoint = &cfg.Endpoint
		}
		o.UsePathStyle = cfg.ForcePathStyle
		o.APIOptions = append(o.APIOptions, v4.SwapComputePayloadSHA256ForUnsignedPayloadMiddleware)
	})
	return &backupObjectStore{client: client, bucket: cfg.Bucket}, nil
}

func (s *backupObjectStore) UploadFile(ctx context.Context, key, path, contentType string) error {
	file, err := os.Open(path)
	if err != nil {
		return err
	}
	defer func() { _ = file.Close() }()
	_, err = s.client.PutObject(ctx, &s3.PutObjectInput{
		Bucket:      &s.bucket,
		Key:         &key,
		Body:        file,
		ContentType: &contentType,
	})
	if err != nil {
		return fmt.Errorf("s3_put_object: %w", err)
	}
	return nil
}

func (s *backupObjectStore) DownloadFile(ctx context.Context, key, path string) error {
	result, err := s.client.GetObject(ctx, &s3.GetObjectInput{
		Bucket: &s.bucket,
		Key:    &key,
	})
	if err != nil {
		return fmt.Errorf("s3_get_object: %w", err)
	}
	defer func() { _ = result.Body.Close() }()
	file, err := os.Create(path)
	if err != nil {
		return err
	}
	defer func() { _ = file.Close() }()
	if _, err := io.Copy(file, result.Body); err != nil {
		return err
	}
	return nil
}

func (s *backupObjectStore) Delete(ctx context.Context, key string) error {
	_, err := s.client.DeleteObject(ctx, &s3.DeleteObjectInput{
		Bucket: &s.bucket,
		Key:    &key,
	})
	return err
}

func (s *backupObjectStore) PresignURL(ctx context.Context, key string, expiry time.Duration) (string, error) {
	presignClient := s3.NewPresignClient(s.client)
	result, err := presignClient.PresignGetObject(ctx, &s3.GetObjectInput{
		Bucket: &s.bucket,
		Key:    &key,
	}, s3.WithPresignExpires(expiry))
	if err != nil {
		return "", fmt.Errorf("s3_presign: %w", err)
	}
	return result.URL, nil
}

func (s *backupObjectStore) HeadBucket(ctx context.Context) error {
	_, err := s.client.HeadBucket(ctx, &s3.HeadBucketInput{Bucket: &s.bucket})
	if err != nil {
		return fmt.Errorf("s3_head_bucket: %w", err)
	}
	return nil
}

func buildBackupS3Key(cfg BackupS3Config, fileName string) string {
	prefix := strings.Trim(strings.TrimSpace(cfg.Prefix), "/")
	if prefix == "" {
		prefix = "cloud-backups"
	}
	return fmt.Sprintf("%s/%s/%s", prefix, time.Now().UTC().Format("2006/01/02"), fileName)
}

func backupPublicID() string {
	return "cbk_" + strings.ReplaceAll(uuid.NewString(), "-", "")[:16]
}

func fileDigest(path string) (int64, string, error) {
	file, err := os.Open(path)
	if err != nil {
		return 0, "", err
	}
	defer func() { _ = file.Close() }()
	hash := sha256.New()
	size, err := io.Copy(hash, file)
	if err != nil {
		return 0, "", err
	}
	return size, hex.EncodeToString(hash.Sum(nil)), nil
}
