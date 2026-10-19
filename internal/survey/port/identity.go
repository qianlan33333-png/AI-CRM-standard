package port

import (
	identityport "github.com/qianlan33333-png/AI-CRM-standard/internal/identity/port"
)

// IdentityCoordinator is implemented at the composition boundary. Resolve
// never provisions; provisioning accepts only an internally constructed,
// Provider-verified fact.
type IdentityCoordinator interface {
	identityport.Resolver
	identityport.VerifiedProvisioner
}
