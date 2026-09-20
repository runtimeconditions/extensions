package normalizer

import "fmt"

const (
	ModelAPIVersion   = "runtimeconditions.io/binding-model/v1alpha1"
	ModelKind         = "RuntimeConditionsBindingModel"
	ExtensionKind     = "RuntimeConditionsExtensionDefinition"
	NormalizerName    = "rc-binding-model"
	NormalizerVersion = "0.1.0"
)

// ExtensionDefinition is the language-neutral source document consumed by the
// resolver. Schema documents stay as YAML/JSON-compatible data rather than
// being interpreted by language emitters.
type ExtensionDefinition struct {
	APIVersion string            `yaml:"apiVersion" json:"apiVersion"`
	Kind       string            `yaml:"kind" json:"kind"`
	Metadata   ExtensionMetadata `yaml:"metadata" json:"metadata"`
	Spec       ExtensionSpec     `yaml:"spec" json:"spec"`
}

type ExtensionMetadata struct {
	ID             string `yaml:"id" json:"id"`
	Version        string `yaml:"version,omitempty" json:"version,omitempty"`
	SemanticSHA256 string `yaml:"semanticSha256,omitempty" json:"semanticSha256,omitempty"`
}

type ExtensionSpec struct {
	Dependencies    []string                   `yaml:"dependencies,omitempty" json:"dependencies,omitempty"`
	Kinds           []KindDefinition           `yaml:"kinds,omitempty" json:"kinds,omitempty"`
	InterfaceTypes  []InterfaceTypeDefinition  `yaml:"interfaceTypes,omitempty" json:"interfaceTypes,omitempty"`
	ConditionFields []ConditionFieldDefinition `yaml:"conditionFields,omitempty" json:"conditionFields,omitempty"`
	InterfaceFields []InterfaceFieldDefinition `yaml:"interfaceFields,omitempty" json:"interfaceFields,omitempty"`
	FieldValues     []FieldValuesDefinition    `yaml:"fieldValues,omitempty" json:"fieldValues,omitempty"`
	Schemas         []ExtensionSchema          `yaml:"schemas,omitempty" json:"schemas,omitempty"`
}

type KindDefinition struct {
	Name string `yaml:"name" json:"name"`
}

type InterfaceTypeDefinition struct {
	Name       string `yaml:"name" json:"name"`
	TargetKind string `yaml:"targetKind" json:"targetKind"`
}

type ConditionFieldDefinition struct {
	Name                    string   `yaml:"name" json:"name"`
	AppliesToKinds          []string `yaml:"appliesToKinds" json:"appliesToKinds"`
	AppliesToInterfaceTypes []string `yaml:"appliesToInterfaceTypes,omitempty" json:"appliesToInterfaceTypes,omitempty"`
}

type InterfaceFieldDefinition struct {
	Name       string `yaml:"name" json:"name"`
	TargetKind string `yaml:"targetKind" json:"targetKind"`
	TargetType string `yaml:"targetType" json:"targetType"`
}

type FieldValuesDefinition struct {
	Field      string `yaml:"field" json:"field"`
	TargetKind string `yaml:"targetKind" json:"targetKind"`
	TargetType string `yaml:"targetType,omitempty" json:"targetType,omitempty"`
	Values     []any  `yaml:"values" json:"values"`
}

type ExtensionSchema struct {
	ID                     string         `yaml:"id" json:"id"`
	Description            string         `yaml:"description" json:"description"`
	AppliesToKind          string         `yaml:"appliesToKind,omitempty" json:"appliesToKind,omitempty"`
	AppliesToInterfaceType string         `yaml:"appliesToInterfaceType,omitempty" json:"appliesToInterfaceType,omitempty"`
	Schema                 map[string]any `yaml:"schema" json:"schema"`
}

type BindingModel struct {
	APIVersion        string              `yaml:"apiVersion" json:"apiVersion"`
	Kind              string              `yaml:"kind" json:"kind"`
	Metadata          ModelMetadata       `yaml:"metadata" json:"metadata"`
	CoreProfileSchema CoreProfileIdentity `yaml:"coreProfileSchema" json:"coreProfileSchema"`
	RootExtension     ArtifactIdentity    `yaml:"rootExtension" json:"rootExtension"`
	Extensions        []ResolvedExtension `yaml:"extensions" json:"extensions"`
	DependencyEdges   []DependencyEdge    `yaml:"dependencyEdges,omitempty" json:"dependencyEdges,omitempty"`
	Vocabulary        VocabularyModel     `yaml:"vocabulary" json:"vocabulary"`
	Scopes            []ScopeModel        `yaml:"scopes,omitempty" json:"scopes,omitempty"`
	Schemas           []NormalizedSchema  `yaml:"schemas,omitempty" json:"schemas,omitempty"`
}

type ModelMetadata struct {
	SemanticSHA256 string       `yaml:"semanticSha256" json:"semanticSha256"`
	Normalizer     ToolIdentity `yaml:"normalizer" json:"normalizer"`
}

type ToolIdentity struct {
	Name    string `yaml:"name" json:"name"`
	Version string `yaml:"version" json:"version"`
	SHA256  string `yaml:"sha256" json:"sha256"`
}

type ArtifactIdentity struct {
	ID             string `yaml:"id" json:"id"`
	Version        string `yaml:"version,omitempty" json:"version,omitempty"`
	SemanticSHA256 string `yaml:"semanticSha256" json:"semanticSha256"`
}

type CoreProfileIdentity struct {
	ID             string `yaml:"id" json:"id"`
	Version        string `yaml:"version" json:"version"`
	SemanticSHA256 string `yaml:"semanticSha256" json:"semanticSha256"`
}

type ResolvedExtension struct {
	ID             string   `yaml:"id" json:"id"`
	Version        string   `yaml:"version,omitempty" json:"version,omitempty"`
	SemanticSHA256 string   `yaml:"semanticSha256" json:"semanticSha256"`
	Dependencies   []string `yaml:"dependencies,omitempty" json:"dependencies,omitempty"`
}

// DependencyLock is exact source-resolution evidence. It is intentionally not
// embedded in BindingModel and never contributes to the model digest.
type DependencyLock struct {
	Extensions []DependencyLockEntry `yaml:"extensions" json:"extensions"`
}

type DependencyLockEntry struct {
	ID             string   `yaml:"id" json:"id"`
	Version        string   `yaml:"version,omitempty" json:"version,omitempty"`
	SourceSHA256   string   `yaml:"sourceSha256" json:"sourceSha256"`
	SemanticSHA256 string   `yaml:"semanticSha256" json:"semanticSha256"`
	SourceBackend  string   `yaml:"sourceBackend" json:"sourceBackend"`
	SourceLocator  string   `yaml:"sourceLocator" json:"sourceLocator"`
	Dependencies   []string `yaml:"dependencies,omitempty" json:"dependencies,omitempty"`
}

type DependencyEdge struct {
	From string `yaml:"from" json:"from"`
	To   string `yaml:"to" json:"to"`
}

type VocabularyModel struct {
	Owners               []VocabularyOwner  `yaml:"owners,omitempty" json:"owners,omitempty"`
	OwnedDeclarations    []DeclarationModel `yaml:"ownedDeclarations,omitempty" json:"ownedDeclarations,omitempty"`
	ImportedDeclarations []DeclarationModel `yaml:"importedDeclarations,omitempty" json:"importedDeclarations,omitempty"`
	Interfaces           []InterfaceModel   `yaml:"interfaces,omitempty" json:"interfaces,omitempty"`
	ConditionFields      []FieldModel       `yaml:"conditionFields,omitempty" json:"conditionFields,omitempty"`
	InterfaceFields      []FieldModel       `yaml:"interfaceFields,omitempty" json:"interfaceFields,omitempty"`
	ValueDomains         []ValueDomainModel `yaml:"valueDomains,omitempty" json:"valueDomains,omitempty"`
}

type VocabularyOwner struct {
	Coordinate    string `yaml:"coordinate" json:"coordinate"`
	Category      string `yaml:"category" json:"category"`
	Owner         string `yaml:"owner" json:"owner"`
	Kind          string `yaml:"kind,omitempty" json:"kind,omitempty"`
	InterfaceType string `yaml:"interfaceType,omitempty" json:"interfaceType,omitempty"`
	Path          string `yaml:"path,omitempty" json:"path,omitempty"`
	Value         any    `yaml:"value,omitempty" json:"value,omitempty"`
}

type DeclarationModel struct {
	Coordinate string     `yaml:"coordinate" json:"coordinate"`
	Owner      string     `yaml:"owner" json:"owner"`
	Kind       string     `yaml:"kind" json:"kind"`
	Tokens     []string   `yaml:"tokens" json:"tokens"`
	Provenance Provenance `yaml:"provenance" json:"provenance"`
}

type InterfaceModel struct {
	Coordinate string     `yaml:"coordinate" json:"coordinate"`
	Owner      string     `yaml:"owner" json:"owner"`
	Kind       string     `yaml:"kind" json:"kind"`
	Type       string     `yaml:"type" json:"type"`
	Tokens     []string   `yaml:"tokens" json:"tokens"`
	Provenance Provenance `yaml:"provenance" json:"provenance"`
}

type FieldModel struct {
	Coordinate    string        `yaml:"coordinate" json:"coordinate"`
	Owner         string        `yaml:"owner" json:"owner"`
	Kind          string        `yaml:"kind" json:"kind"`
	InterfaceType string        `yaml:"interfaceType,omitempty" json:"interfaceType,omitempty"`
	Path          string        `yaml:"path" json:"path"`
	Segments      []PathSegment `yaml:"segments" json:"segments"`
	Tokens        []string      `yaml:"tokens" json:"tokens"`
	Provenance    Provenance    `yaml:"provenance" json:"provenance"`
}

type ValueDomainModel struct {
	Coordinate    string            `yaml:"coordinate" json:"coordinate"`
	Owner         string            `yaml:"owner" json:"owner"`
	Kind          string            `yaml:"kind" json:"kind"`
	InterfaceType string            `yaml:"interfaceType,omitempty" json:"interfaceType,omitempty"`
	Path          string            `yaml:"path" json:"path"`
	Segments      []PathSegment     `yaml:"segments" json:"segments"`
	Values        []NormalizedValue `yaml:"values" json:"values"`
	Provenance    Provenance        `yaml:"provenance" json:"provenance"`
}

type NormalizedValue struct {
	Value  any      `yaml:"value" json:"value"`
	Tokens []string `yaml:"tokens,omitempty" json:"tokens,omitempty"`
}

type PathSegment struct {
	Name   string   `yaml:"name" json:"name"`
	Array  bool     `yaml:"array,omitempty" json:"array,omitempty"`
	Tokens []string `yaml:"tokens" json:"tokens"`
}

type ScopeModel struct {
	Coordinate        string   `yaml:"coordinate" json:"coordinate"`
	Kind              string   `yaml:"kind" json:"kind"`
	InterfaceType     string   `yaml:"interfaceType,omitempty" json:"interfaceType,omitempty"`
	ApplicableSchemas []string `yaml:"applicableSchemas,omitempty" json:"applicableSchemas,omitempty"`
	Projection        *Shape   `yaml:"projection,omitempty" json:"projection,omitempty"`
}

type NormalizedSchema struct {
	Coordinate    string         `yaml:"coordinate" json:"coordinate"`
	Owner         string         `yaml:"owner" json:"owner"`
	ID            string         `yaml:"id" json:"id"`
	Kind          string         `yaml:"kind,omitempty" json:"kind,omitempty"`
	InterfaceType string         `yaml:"interfaceType,omitempty" json:"interfaceType,omitempty"`
	Exact         map[string]any `yaml:"exact" json:"exact"`
	Projection    Shape          `yaml:"projection" json:"projection"`
	Definitions   []NamedShape   `yaml:"definitions,omitempty" json:"definitions,omitempty"`
	Provenance    Provenance     `yaml:"provenance" json:"provenance"`
}

type NamedShape struct {
	Name        string     `yaml:"name" json:"name"`
	Tokens      []string   `yaml:"tokens" json:"tokens"`
	JSONPointer string     `yaml:"jsonPointer" json:"jsonPointer"`
	Shape       Shape      `yaml:"shape" json:"shape"`
	Provenance  Provenance `yaml:"provenance" json:"provenance"`
}

type Shape struct {
	Kind        string            `yaml:"kind" json:"kind"`
	Scalar      string            `yaml:"scalar,omitempty" json:"scalar,omitempty"`
	Ref         string            `yaml:"ref,omitempty" json:"ref,omitempty"`
	Required    []string          `yaml:"required,omitempty" json:"required,omitempty"`
	Properties  []PropertyShape   `yaml:"properties,omitempty" json:"properties,omitempty"`
	Items       *Shape            `yaml:"items,omitempty" json:"items,omitempty"`
	MapValues   *Shape            `yaml:"mapValues,omitempty" json:"mapValues,omitempty"`
	Variants    []Shape           `yaml:"variants,omitempty" json:"variants,omitempty"`
	Values      []NormalizedValue `yaml:"values,omitempty" json:"values,omitempty"`
	Constraints map[string]any    `yaml:"constraints,omitempty" json:"constraints,omitempty"`
	Provenance  Provenance        `yaml:"provenance" json:"provenance"`
}

type PropertyShape struct {
	Name       string     `yaml:"name" json:"name"`
	Tokens     []string   `yaml:"tokens" json:"tokens"`
	Required   bool       `yaml:"required,omitempty" json:"required,omitempty"`
	Shape      Shape      `yaml:"shape" json:"shape"`
	Provenance Provenance `yaml:"provenance" json:"provenance"`
}

type Provenance struct {
	Owner           string `yaml:"owner" json:"owner"`
	ExtensionSHA256 string `yaml:"extensionSha256" json:"extensionSha256"`
	Coordinate      string `yaml:"coordinate" json:"coordinate"`
	JSONPointer     string `yaml:"jsonPointer,omitempty" json:"jsonPointer,omitempty"`
}

type Diagnostic struct {
	Category    string `yaml:"category" json:"category"`
	Code        string `yaml:"code" json:"code"`
	Coordinate  string `yaml:"coordinate,omitempty" json:"coordinate,omitempty"`
	JSONPointer string `yaml:"jsonPointer,omitempty" json:"jsonPointer,omitempty"`
	Message     string `yaml:"message" json:"message"`
}

type DiagnosticError struct {
	Diagnostic Diagnostic
}

func (e *DiagnosticError) Error() string {
	d := e.Diagnostic
	location := d.Coordinate
	if d.JSONPointer != "" {
		if location != "" {
			location += " "
		}
		location += d.JSONPointer
	}
	if location == "" {
		return fmt.Sprintf("%s %s: %s", d.Category, d.Code, d.Message)
	}
	return fmt.Sprintf("%s %s at %s: %s", d.Category, d.Code, location, d.Message)
}

type ResolvedDocument struct {
	Definition     ExtensionDefinition
	Data           map[string]any
	SemanticData   map[string]any
	Bytes          []byte
	SourceSHA256   string
	SemanticSHA256 string
	Backend        string
	Locator        string
}

type ResolvedClosure struct {
	Root      string
	Documents []ResolvedDocument
	ByID      map[string]ResolvedDocument
	Edges     []DependencyEdge
}
