package admin

import "testing"

func TestSanitizeDatabaseTextRemovesHiddenControlCharacters(t *testing.T) {
	input := " \x00订单\tA\n备注\x1f "
	got := sanitizeDatabaseText(input)
	want := "订单\tA\n备注"
	if got != want {
		t.Fatalf("sanitizeDatabaseText() = %q, want %q", got, want)
	}
}

func TestNormalizeAuditReasonSanitizesBeforeValidation(t *testing.T) {
	got := normalizeAuditReason("\x00", "  正常原因\x00  ")
	want := "正常原因"
	if got != want {
		t.Fatalf("normalizeAuditReason() = %q, want %q", got, want)
	}
}
