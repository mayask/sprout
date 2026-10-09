package sprout

import (
	"fmt"
	"slices"
	"sort"

	"github.com/getkin/kin-openapi/openapi3"
)

// securityRequirement is one alternative of an operation's security: a scheme
// name and the scopes it requires (sorted, deduplicated).
type securityRequirement struct {
	scheme string
	scopes []string
}

func newSecurityRequirement(scheme string, scopes []string) securityRequirement {
	sorted := append([]string(nil), scopes...)
	sort.Strings(sorted)
	return securityRequirement{scheme: scheme, scopes: slices.Compact(sorted)}
}

func (r securityRequirement) equal(other securityRequirement) bool {
	return r.scheme == other.scheme && slices.Equal(r.scopes, other.scopes)
}

// securityLayer is a router's documented security default, ordered like
// middleware so it only affects routes registered after it.
type securityLayer struct {
	order       int64
	requirement securityRequirement
}

func toOpenAPISecurity(requirements []securityRequirement) openapi3.SecurityRequirements {
	out := make(openapi3.SecurityRequirements, 0, len(requirements))
	for _, r := range requirements {
		scopes := r.scopes
		if scopes == nil {
			scopes = []string{} // serializes as [] rather than null
		}
		out = append(out, openapi3.SecurityRequirement{r.scheme: scopes})
	}
	return out
}

// WithOpenAPISecurityScheme declares components.securitySchemes[name] on the
// router's document. On Mount it requires WithOpenAPIDocument. Declare schemes
// before registering routes that reference them.
func WithOpenAPISecurityScheme(name string, scheme *openapi3.SecurityScheme) Option {
	return func(cfg *Config) {
		if name == "" || scheme == nil {
			panic("sprout: WithOpenAPISecurityScheme requires a name and a scheme")
		}
		if cfg.openapi.securitySchemes == nil {
			cfg.openapi.securitySchemes = make(map[string]*openapi3.SecurityScheme)
		}
		cfg.openapi.securitySchemes[name] = scheme
	}
}

// WithOpenAPISecurity sets the document's top-level security requirement.
// Operations whose resolved security equals it omit their own security
// field; all others document theirs explicitly, so routes without security
// get security: []. It does not secure routes by itself: use
// UseOpenAPISecurity or WithSecurity for that. On Mount it requires
// WithOpenAPIDocument. Unknown schemes or scopes panic.
func WithOpenAPISecurity(scheme string, scopes ...string) Option {
	return func(cfg *Config) {
		requirement := newSecurityRequirement(scheme, scopes)
		cfg.openapi.security = &requirement
	}
}

// UseOpenAPISecurity documents scheme (with optional scopes) as the security
// requirement of routes registered on this router and its descendants after
// this call, mirroring Use ordering. A later call replaces the default for
// subsequent routes. It only affects documentation and stops at
// WithOpenAPIDocument boundaries; enforce authentication in middleware.
// Unknown schemes or scopes panic.
func (s *Sprout) UseOpenAPISecurity(scheme string, scopes ...string) {
	requirement := newSecurityRequirement(scheme, scopes)
	if err := s.openapi.validateSecurityRequirement(requirement); err != nil {
		panic(err)
	}
	layer := &securityLayer{order: s.order.Next(), requirement: requirement}
	s.mwMu.Lock()
	s.openapiSecurity = layer
	s.mwMu.Unlock()
}

// WithSecurity documents that the route requires scheme with the given scopes,
// replacing the router default. Repeat it to document alternatives (any one
// suffices). Unknown schemes or scopes panic.
func WithSecurity(scheme string, scopes ...string) RouteOption {
	return func(cfg *routeConfig) {
		cfg.security = append(cfg.security, newSecurityRequirement(scheme, scopes))
	}
}

// WithoutSecurity documents the route as public (security: []), overriding
// any router default.
func WithoutSecurity() RouteOption {
	return func(cfg *routeConfig) {
		cfg.noSecurity = true
	}
}

// inheritedOpenAPISecurity returns the most recently set UseOpenAPISecurity
// default among this router and its ancestors writing to the same document.
func (s *Sprout) inheritedOpenAPISecurity() *securityRequirement {
	var latest *securityLayer
	for router := s; router != nil && router.openapi == s.openapi; router = router.parent {
		router.mwMu.RLock()
		layer := router.openapiSecurity
		router.mwMu.RUnlock()
		if layer != nil && (latest == nil || layer.order > latest.order) {
			latest = layer
		}
	}
	if latest == nil {
		return nil
	}
	return &latest.requirement
}

// setSecurity installs schemes and the top-level requirement at construction.
func (d *openAPIDocument) setSecurity(schemes map[string]*openapi3.SecurityScheme, security *securityRequirement) {
	if len(schemes) > 0 {
		d.doc.Components.SecuritySchemes = make(openapi3.SecuritySchemes, len(schemes))
		for name, scheme := range schemes {
			d.doc.Components.SecuritySchemes[name] = &openapi3.SecuritySchemeRef{Value: scheme}
		}
	}
	if security != nil {
		if err := d.validateSecurityRequirement(*security); err != nil {
			panic(err)
		}
		d.security = security
		d.doc.Security = toOpenAPISecurity([]securityRequirement{*security})
	}
}

// validateSecurityRequirement checks the scheme is declared and the scopes are
// allowed: declared by one of the scheme's OAuth2 flows, unchecked for
// openIdConnect, and absent for other scheme types. Schemes are fixed at
// document construction, so no lock is needed.
func (d *openAPIDocument) validateSecurityRequirement(r securityRequirement) error {
	ref := d.doc.Components.SecuritySchemes[r.scheme]
	if ref == nil || ref.Value == nil {
		return fmt.Errorf("sprout: unknown OpenAPI security scheme %q; declare it with WithOpenAPISecurityScheme", r.scheme)
	}
	scheme := ref.Value
	switch scheme.Type {
	case "oauth2":
		for _, scope := range r.scopes {
			if !oauthFlowsDeclareScope(scheme.Flows, scope) {
				return fmt.Errorf("sprout: OAuth2 security scheme %q does not declare scope %q", r.scheme, scope)
			}
		}
	case "openIdConnect":
		// Scopes come from the provider's discovery document.
	default:
		if len(r.scopes) > 0 {
			return fmt.Errorf("sprout: security scheme %q of type %q does not take scopes", r.scheme, scheme.Type)
		}
	}
	return nil
}

func oauthFlowsDeclareScope(flows *openapi3.OAuthFlows, scope string) bool {
	if flows == nil {
		return false
	}
	for _, flow := range []*openapi3.OAuthFlow{flows.Implicit, flows.Password, flows.ClientCredentials, flows.AuthorizationCode} {
		if flow == nil {
			continue
		}
		if _, ok := flow.Scopes[scope]; ok {
			return true
		}
	}
	return false
}

// operationSecurity validates a route's resolved requirements and returns the
// operation-level security to document: nil when it matches the document's
// top-level requirement (or both are empty), otherwise the explicit list, with
// an empty list meaning public.
func (d *openAPIDocument) operationSecurity(requirements []securityRequirement) *openapi3.SecurityRequirements {
	for _, r := range requirements {
		if err := d.validateSecurityRequirement(r); err != nil {
			panic(err)
		}
	}
	if d.security == nil {
		if len(requirements) == 0 {
			return nil
		}
	} else if len(requirements) == 1 && requirements[0].equal(*d.security) {
		return nil
	}
	out := toOpenAPISecurity(requirements)
	return &out
}
