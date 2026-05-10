package billing

// ExportComputeStripeSignature exposes computeStripeSignature for
// tests in the billing_test package.
func ExportComputeStripeSignature(secret []byte, timestamp int64, payload []byte) string {
	return computeStripeSignature(secret, timestamp, payload)
}
