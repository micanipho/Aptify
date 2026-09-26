package domain

// The stack of the *generated* apps. Declared values are wider than implemented ones on
// purpose (ADR-0001): the enums document direction, and CheckSupported rejects every
// unimplemented value at the request boundary.

type Framework string

const (
	FrameworkNextJS    Framework = "nextjs"
	FrameworkReactVite Framework = "react-vite"
	FrameworkVue       Framework = "vue"
	FrameworkAngular   Framework = "angular"
	FrameworkBlazor    Framework = "blazor"
)

type Language string

const (
	LanguageTypeScript Language = "typescript"
	LanguageJavaScript Language = "javascript"
	LanguageCSharp     Language = "csharp"
)

type Database string

const (
	DatabasePostgres Database = "postgres"
	DatabaseMongoDB  Database = "mongodb"
	DatabaseSQLite   Database = "sqlite"
)

type GitProvider string

const (
	GitProviderGitHub      GitProvider = "github"
	GitProviderGitLab      GitProvider = "gitlab"
	GitProviderBitbucket   GitProvider = "bitbucket"
	GitProviderAzureDevOps GitProvider = "azure-devops"
)

type DeployTarget string

const (
	DeployTargetVercel  DeployTarget = "vercel"
	DeployTargetNetlify DeployTarget = "netlify"
	DeployTargetRailway DeployTarget = "railway"
	DeployTargetRender  DeployTarget = "render"
	DeployTargetAzure   DeployTarget = "azure"
	DeployTargetNone    DeployTarget = "none"
)

// TargetStack is the stack a project's generated app is built on.
type TargetStack struct {
	Framework    Framework    `json:"framework"`
	Language     Language     `json:"language"`
	Database     Database     `json:"database"`
	GitProvider  GitProvider  `json:"gitProvider"`
	DeployTarget DeployTarget `json:"deployTarget"`
}

// DefaultTarget is nextjs / typescript / postgres / github / none.
func DefaultTarget() TargetStack {
	return TargetStack{
		Framework:    FrameworkNextJS,
		Language:     LanguageTypeScript,
		Database:     DatabasePostgres,
		GitProvider:  GitProviderGitHub,
		DeployTarget: DeployTargetNone,
	}
}

// WithDefaults fills every empty dimension from DefaultTarget, so a request may send a
// partial target.
func (t TargetStack) WithDefaults() TargetStack {
	d := DefaultTarget()
	if t.Framework == "" {
		t.Framework = d.Framework
	}
	if t.Language == "" {
		t.Language = d.Language
	}
	if t.Database == "" {
		t.Database = d.Database
	}
	if t.GitProvider == "" {
		t.GitProvider = d.GitProvider
	}
	if t.DeployTarget == "" {
		t.DeployTarget = d.DeployTarget
	}
	return t
}

// CheckSupported returns an unsupported_target error naming the first dimension whose value
// has no implementation. Call it before a run is created, never inside a step.
func (t TargetStack) CheckSupported() error {
	switch {
	case t.Framework != FrameworkNextJS:
		return unsupported("framework", string(t.Framework))
	case t.Language != LanguageTypeScript:
		return unsupported("language", string(t.Language))
	case t.Database != DatabasePostgres:
		return unsupported("database", string(t.Database))
	case t.GitProvider != GitProviderGitHub:
		return unsupported("gitProvider", string(t.GitProvider))
	case t.DeployTarget != DeployTargetVercel && t.DeployTarget != DeployTargetNone:
		return unsupported("deployTarget", string(t.DeployTarget))
	}
	return nil
}

func unsupported(dimension, value string) error {
	return Errorf(CodeUnsupportedTarget, "%s %q is not supported", dimension, value)
}
