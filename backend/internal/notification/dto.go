package notification

// ResendResponse is the body of POST /api/v1/admin/orders/:id/resend-email.
//
// It echoes the address the tickets went to so an admin can confirm at a glance
// that support reached the right buyer.
type ResendResponse struct {
	Message string `json:"message"`
	SentTo  string `json:"sent_to"`
}
