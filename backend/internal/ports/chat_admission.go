package ports

// ChatAdmissionError carries an operator-facing reason classified by the
// governance boundary. Unexpected provider/storage errors must not populate
// Reason: those errors may contain private request or credential material.
type ChatAdmissionError struct {
	Reason string
}

func (e *ChatAdmissionError) Error() string { return e.Reason }
