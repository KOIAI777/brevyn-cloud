package operations

import (
	"context"
	"errors"
	"log/slog"
	"sync"
	"time"

	"github.com/brevyn/brevyn-cloud/internal/gateway/gatewayerror"
	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"
)

type OperationRecord struct {
	DBID        int64
	PublicID    string
	Operation   string
	TargetType  string
	TargetID    string
	Payload     string
	Attempts    int
	MaxAttempts int
}

type Executor interface {
	ExecuteGatewayOperation(ctx context.Context, op OperationRecord) (map[string]any, error)
}

type Runner struct {
	postgres    *pgxpool.Pool
	logger      *slog.Logger
	executor    Executor
	workerID    string
	interval    time.Duration
	batchSize   int
	concurrency int
	staleAfter  time.Duration
}

type RunnerOption func(*Runner)

func WithInterval(interval time.Duration) RunnerOption {
	return func(r *Runner) {
		if interval > 0 {
			r.interval = interval
		}
	}
}

func WithBatchSize(batchSize int) RunnerOption {
	return func(r *Runner) {
		if batchSize > 0 {
			r.batchSize = batchSize
		}
	}
}

func WithConcurrency(concurrency int) RunnerOption {
	return func(r *Runner) {
		if concurrency > 0 {
			r.concurrency = concurrency
		}
	}
}

func WithStaleTimeout(timeout time.Duration) RunnerOption {
	return func(r *Runner) {
		if timeout > 0 {
			r.staleAfter = timeout
		}
	}
}

func NewRunner(postgres *pgxpool.Pool, logger *slog.Logger, executor Executor, opts ...RunnerOption) *Runner {
	if logger == nil {
		logger = slog.Default()
	}
	runner := &Runner{
		postgres:    postgres,
		logger:      logger,
		executor:    executor,
		workerID:    "worker-" + uuid.NewString(),
		interval:    10 * time.Second,
		batchSize:   10,
		concurrency: 5,
		staleAfter:  5 * time.Minute,
	}
	for _, opt := range opts {
		if opt != nil {
			opt(runner)
		}
	}
	if runner.batchSize < 1 {
		runner.batchSize = 1
	}
	if runner.concurrency < 1 {
		runner.concurrency = 1
	}
	return runner
}

func (r *Runner) WorkerID() string {
	return r.workerID
}

func (r *Runner) Run(ctx context.Context) error {
	ticker := time.NewTicker(r.interval)
	defer ticker.Stop()

	for {
		if err := r.ProcessOnce(ctx); err != nil && !errors.Is(err, context.Canceled) {
			r.logger.Warn("gateway operations process failed", "error", err)
		}
		select {
		case <-ctx.Done():
			return ctx.Err()
		case <-ticker.C:
		}
	}
}

func (r *Runner) ProcessOnce(ctx context.Context) error {
	if err := r.requeueStaleRunning(ctx); err != nil {
		return err
	}

	ops := make([]OperationRecord, 0, r.batchSize)
	for i := 0; i < r.batchSize; i++ {
		op, ok, err := r.claimNext(ctx)
		if err != nil {
			return err
		}
		if !ok {
			break
		}
		ops = append(ops, op)
	}
	if len(ops) == 0 {
		return nil
	}

	sem := make(chan struct{}, r.concurrency)
	var wg sync.WaitGroup
	for _, op := range ops {
		select {
		case <-ctx.Done():
			return ctx.Err()
		case sem <- struct{}{}:
		}
		wg.Add(1)
		go func(op OperationRecord) {
			defer wg.Done()
			defer func() { <-sem }()
			r.processClaimed(ctx, op)
		}(op)
	}
	wg.Wait()
	return nil
}

func (r *Runner) requeueStaleRunning(ctx context.Context) error {
	if r.staleAfter <= 0 {
		return nil
	}
	cutoff := time.Now().UTC().Add(-r.staleAfter)
	tag, err := r.postgres.Exec(ctx, `
		UPDATE gateway_operations
		SET status = CASE WHEN attempts >= max_attempts THEN 'dead_letter' ELSE 'failed' END,
			last_error_message = 'worker lock expired; operation requeued',
			last_error_code = 'gateway_operation_stale',
			last_error_class = 'retryable',
			last_error_stage = 'worker',
			last_error_retryable = attempts < max_attempts,
			last_error_detail = concat('locked_by=', coalesce(locked_by, ''), '; stale_after=', $2::text),
			next_run_at = now(),
			completed_at = CASE WHEN attempts >= max_attempts THEN now() ELSE completed_at END,
			locked_at = NULL,
			locked_by = '',
			updated_at = now()
		WHERE status = 'running'
			AND locked_at IS NOT NULL
			AND locked_at < $1
	`, cutoff, r.staleAfter.String())
	if err != nil {
		return err
	}
	if rows := tag.RowsAffected(); rows > 0 {
		r.logger.Warn("requeued stale gateway operations", "count", rows, "stale_after", r.staleAfter.String())
	}
	return nil
}

func (r *Runner) claimNext(ctx context.Context) (OperationRecord, bool, error) {
	var op OperationRecord
	err := r.postgres.QueryRow(ctx, `
		WITH candidate AS (
			SELECT id
			FROM gateway_operations
			WHERE status IN ('pending', 'failed')
				AND next_run_at <= now()
				AND attempts < max_attempts
				AND (status = 'pending' OR last_error_retryable = true)
			ORDER BY next_run_at ASC, id ASC
			LIMIT 1
			FOR UPDATE SKIP LOCKED
		)
		UPDATE gateway_operations go
		SET status = 'running',
			attempts = attempts + 1,
			locked_at = now(),
			locked_by = $1,
			started_at = coalesce(started_at, now()),
			updated_at = now()
		FROM candidate
		WHERE go.id = candidate.id
		RETURNING go.id, go.public_id, go.operation, go.target_type, go.target_id, go.payload::text, go.attempts, go.max_attempts
	`, r.workerID).Scan(
		&op.DBID,
		&op.PublicID,
		&op.Operation,
		&op.TargetType,
		&op.TargetID,
		&op.Payload,
		&op.Attempts,
		&op.MaxAttempts,
	)
	if errors.Is(err, pgx.ErrNoRows) {
		return OperationRecord{}, false, nil
	}
	return op, err == nil, err
}

func (r *Runner) processClaimed(ctx context.Context, op OperationRecord) {
	runCtx, cancel := context.WithTimeout(ctx, 90*time.Second)
	defer cancel()

	result, err := r.executor.ExecuteGatewayOperation(runCtx, op)
	if err == nil {
		if markErr := MarkSucceeded(ctx, r.postgres, op.PublicID, result); markErr != nil {
			r.logger.Warn("mark gateway operation succeeded failed", "operation", op.PublicID, "error", markErr)
		}
		return
	}

	info := gatewayerror.Classify(op.Operation, err)
	deadLetter := !info.Retryable || op.Attempts >= op.MaxAttempts
	nextRunAt := time.Now().UTC().Add(Backoff(op.Attempts))
	if deadLetter {
		nextRunAt = time.Now().UTC()
	}
	if markErr := MarkFailed(ctx, r.postgres, op.PublicID, info, nextRunAt, deadLetter); markErr != nil {
		r.logger.Warn("mark gateway operation failed failed", "operation", op.PublicID, "error", markErr)
	}
}
