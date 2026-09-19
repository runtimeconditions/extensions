// Package envconfiguration is a structural binding-model fixture generated
// from the Environment Configuration extension definition.
package envconfiguration

import common "github.com/runtimeconditions/extensions/tooling/extension-bindings/structural-model/go/commonintegrations"

// Property is the enum represented by the scoped fieldValues entries for
// configuration.env[].property and
// configuration.alternatives[].env[].property.
type Property string

const (
	URL      Property = "url"
	BaseURL  Property = "baseUrl"
	Hostname Property = "hostname"
	Port     Property = "port"
	Scheme   Property = "scheme"
	Username Property = "username"
	Password Property = "password"
	Database Property = "database"
	Token    Property = "token"
	TLS      Property = "tls"
)

// Env is the object at configuration.env[] and at
// configuration.alternatives[].env[].
type Env struct {
	Property  Property
	Name      string
	Sensitive *bool
	Required  *bool
}

// Alternatives is the array at configuration.alternatives.
type Alternatives []AlternativesItem

// AlternativesItem is the object at configuration.alternatives[].
type AlternativesItem struct {
	Env []Env
}

// Configuration is the extension-owned root Condition field. The extension
// schema requires exactly one of Env or Alternatives.
type Configuration struct {
	Env          []Env
	Alternatives Alternatives
}

// These exported markers let the additive field participate in declarations
// for each dependency-owned kind named by the extension scopes.
func (Configuration) RuntimeConditionsAPIField()       {}
func (Configuration) RuntimeConditionsDatastoreField() {}
func (Configuration) RuntimeConditionsCacheField()     {}

var (
	_ common.APIField       = Configuration{}
	_ common.DatastoreField = Configuration{}
	_ common.CacheField     = Configuration{}
)
