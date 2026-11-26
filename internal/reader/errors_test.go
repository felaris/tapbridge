package reader

import (
	"fmt"
	"strings"
	"testing"

	"github.com/ebfe/scard"

	"github.com/felaris/tapbridge/internal/ndef"
)

func TestExplain(t *testing.T) {
	_, tooLong := ndef.BuildText(strings.Repeat("x", 300))
	cases := []struct {
		err  error
		code string
		want string // substring of the message
	}{
		{errCardGone, CodeCardRemoved, "moved away"},
		{fmt.Errorf("write page 5: %w", errCardGone), CodeCardRemoved, "moved away"},
		{scard.ErrRemovedCard, CodeCardRemoved, "moved away"},
		{scard.ErrSharingViolation, CodeReaderBusy, "Another program"},
		{scard.ErrServiceStopped, CodeServiceStopped, "Smart Card"},
		{scard.ErrReaderUnavailable, CodeReaderGone, "disconnected"},
		{&statusError{0x63, 0x00}, CodeUnsupportedCard, "NTAG"},
		{fmt.Errorf("write page 4: %w", &statusError{0x69, 0x82}), CodeReadOnly, "locked"},
		{&statusError{0x6A, 0x81}, CodeUnsupportedCard, "doesn't support"},
		{&statusError{0x12, 0x34}, CodeUnknown, "12 34"},
		{&tagTooSmallError{need: 64, have: 48}, CodeTagTooSmall, "needs 64 bytes"},
		{tooLong, CodeValueTooLong, "too long"},
		{ErrWritePending, CodeWritePending, "already waiting"},
		{fmt.Errorf("boom"), CodeUnknown, "boom"},
	}
	for _, c := range cases {
		e := Explain(c.err)
		if e == nil || e.Code != c.code || !strings.Contains(e.Message, c.want) {
			t.Errorf("Explain(%v) = %+v, want code %s containing %q", c.err, e, c.code, c.want)
		}
	}
	if Explain(nil) != nil {
		t.Error("Explain(nil) must be nil")
	}
	// Already-explained errors pass through unchanged.
	e := &Error{Code: "x", Message: "y"}
	if Explain(e) != e {
		t.Error("Explain must return an existing *Error as-is")
	}
}
