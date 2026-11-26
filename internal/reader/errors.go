package reader

import (
	"errors"
	"fmt"

	"github.com/ebfe/scard"

	"github.com/felaris/tapbridge/internal/ndef"
)

// Error codes sent to clients in write_error messages. They are part of the
// public protocol (see docs/protocol.md): add new ones, never rename.
const (
	CodeCardRemoved     = "card_removed"
	CodeUnsupportedCard = "unsupported_card"
	CodeTagTooSmall     = "tag_too_small"
	CodeValueTooLong    = "value_too_long"
	CodeReadOnly        = "read_only"
	CodeReaderBusy      = "reader_busy"
	CodeReaderGone      = "reader_unavailable"
	CodeServiceStopped  = "service_unavailable"
	CodeWritePending    = "write_pending"
	CodeUnknown         = "unknown"
)

// Error is a failure explained in plain English. Message says what went
// wrong and what the user can do about it; Code is stable for programs.
type Error struct {
	Code    string
	Message string
	Err     error // underlying cause, for logs
}

func (e *Error) Error() string { return e.Message }
func (e *Error) Unwrap() error { return e.Err }

// ErrorCode returns the protocol error code.
func (e *Error) ErrorCode() string { return e.Code }

// statusError is a non-success APDU status word from the card or reader.
type statusError struct{ sw1, sw2 byte }

func (e *statusError) Error() string {
	return fmt.Sprintf("card returned status %02X %02X", e.sw1, e.sw2)
}

// tagTooSmallError means the encoded value does not fit the tag's data area.
type tagTooSmallError struct{ need, have int }

func (e *tagTooSmallError) Error() string {
	return fmt.Sprintf("needs %d bytes, tag holds %d", e.need, e.have)
}

// Explain converts any error from this package into an *Error with a
// readable message. It returns nil for a nil error.
func Explain(err error) *Error {
	if err == nil {
		return nil
	}
	var e *Error
	if errors.As(err, &e) {
		return e
	}
	mk := func(code, msg string) *Error { return &Error{Code: code, Message: msg, Err: err} }

	var tooSmall *tagTooSmallError
	var tooLarge *ndef.TooLargeError
	var sw *statusError
	switch {
	case errors.Is(err, ErrWritePending):
		return mk(CodeWritePending, "Another write is already waiting for a card. Tap a card to finish it first.")
	case errors.As(err, &tooLarge):
		return mk(CodeValueTooLong, fmt.Sprintf(
			"That value is too long to store on an NFC tag (%d bytes; the limit is about %d). Use a shorter value.",
			tooLarge.Size, ndef.MaxRecordLen-8))
	case errors.As(err, &tooSmall):
		return mk(CodeTagTooSmall, fmt.Sprintf(
			"This tag is too small for that value: it needs %d bytes but the tag only holds %d. Use a shorter value or a larger tag (e.g. NTAG215 or NTAG216).",
			tooSmall.need, tooSmall.have))
	case errors.Is(err, errCardGone), isCardGone(err):
		return mk(CodeCardRemoved, "The card was moved away too soon. Hold it flat on the reader until it beeps, then try again.")
	case errors.Is(err, scard.ErrSharingViolation):
		return mk(CodeReaderBusy, "Another program is using the reader. Close other NFC or smart-card apps and try again.")
	case errors.Is(err, scard.ErrUnknownReader), errors.Is(err, scard.ErrReaderUnavailable), errors.Is(err, scard.ErrNoReadersAvailable):
		return mk(CodeReaderGone, "The NFC reader was disconnected. Plug it back in and try again.")
	case errors.Is(err, scard.ErrNoService), errors.Is(err, scard.ErrServiceStopped):
		return mk(CodeServiceStopped, "The system smart-card service isn't running. Unplug and replug the reader; on Windows, make sure the \"Smart Card\" service isn't disabled.")
	case errors.Is(err, scard.ErrUnsupportedCard), errors.Is(err, scard.ErrUnknownCard), errors.Is(err, scard.ErrCardUnsupported), errors.Is(err, scard.ErrProtoMismatch):
		return mk(CodeUnsupportedCard, unsupportedCardMsg)
	case errors.As(err, &sw):
		return explainStatus(sw, err)
	}
	return mk(CodeUnknown, "Something went wrong talking to the card: "+err.Error()+". Try tapping again; if it keeps happening, open the log from the tray menu.")
}

const unsupportedCardMsg = "This card type isn't supported. TapBridge reads and writes NTAG21x / MIFARE Ultralight (NFC Forum Type 2) tags; MIFARE Classic, DESFire, bank and ID cards can only be read by UID."

func explainStatus(sw *statusError, err error) *Error {
	switch {
	case sw.sw1 == 0x69 && (sw.sw2 == 0x81 || sw.sw2 == 0x82 || sw.sw2 == 0x85):
		return &Error{Code: CodeReadOnly, Err: err,
			Message: "The tag refused the write: it is locked or password-protected. Use a blank, unlocked tag."}
	case sw.sw1 == 0x6A && sw.sw2 == 0x81, sw.sw1 == 0x6D, sw.sw1 == 0x6E:
		return &Error{Code: CodeUnsupportedCard, Err: err,
			Message: "The reader doesn't support this command for this card. " + unsupportedCardMsg}
	case sw.sw1 == 0x63:
		// ACR122U and most PC/SC readers return 63 00 when the card NAKs a
		// READ/WRITE — typically a MIFARE Classic card (needs authentication)
		// or a locked page.
		return &Error{Code: CodeUnsupportedCard, Err: err,
			Message: "The card rejected the command. It is probably not an NTAG/Ultralight tag, or it is locked. " + unsupportedCardMsg}
	}
	return &Error{Code: CodeUnknown, Err: err,
		Message: fmt.Sprintf("The card returned an unexpected error (status %02X %02X). Try tapping again or use a different tag.", sw.sw1, sw.sw2)}
}
