// Package payments is an OpenAPI naming fixture: sibling fixture packages
// share the last import-path segment and type names on purpose.
package payments

// PaymentResponse collides by name with the other fixture packages.
type PaymentResponse struct {
	ID   string `json:"id"`
	Beta string `json:"beta_reference"`
}
