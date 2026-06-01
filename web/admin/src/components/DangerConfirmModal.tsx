import { AlertTriangle, X } from "lucide-react";
import { useEffect, useState } from "react";

type DangerConfirmModalProps = {
  open: boolean;
  title: string;
  description: string;
  confirmLabel?: string;
  pending?: boolean;
  reasonPlaceholder?: string;
  onCancel: () => void;
  onConfirm: (reason: string) => void;
};

export function DangerConfirmModal({
  open,
  title,
  description,
  confirmLabel = "确认执行",
  pending = false,
  reasonPlaceholder = "例如：联动小铺订单补发 / 客服确认 / 修复同步失败",
  onCancel,
  onConfirm
}: DangerConfirmModalProps) {
  const [reason, setReason] = useState("");

  useEffect(() => {
    if (open) setReason("");
  }, [open]);

  if (!open) return null;

  const trimmed = reason.trim();

  return (
    <div className="modal-backdrop" role="presentation">
      <section aria-modal="true" className="danger-confirm-modal" role="dialog">
        <div className="modal-heading">
          <div className="modal-icon danger">
            <AlertTriangle size={20} />
          </div>
          <div>
            <h3>{title}</h3>
            <p>{description}</p>
          </div>
          <button aria-label="关闭" className="icon-button" disabled={pending} onClick={onCancel} type="button">
            <X size={18} />
          </button>
        </div>
        <label className="modal-reason-field">
          <span>操作原因</span>
          <textarea
            autoFocus
            maxLength={500}
            onChange={(event) => setReason(event.target.value)}
            placeholder={reasonPlaceholder}
            value={reason}
          />
        </label>
        <div className="modal-footer">
          <span>{trimmed.length}/500</span>
          <div className="button-row">
            <button className="secondary-action" disabled={pending} onClick={onCancel} type="button">
              取消
            </button>
            <button className="danger-action" disabled={pending || trimmed.length === 0} onClick={() => onConfirm(trimmed)} type="button">
              {pending ? "执行中" : confirmLabel}
            </button>
          </div>
        </div>
      </section>
    </div>
  );
}
