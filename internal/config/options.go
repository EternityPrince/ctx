package config

type Options struct {
	Root             string
	ConfigPath       string
	ConfigProfile    string
	IncludeHidden    bool
	MaxFileSize      int64
	OutputPath       string
	CopyToClipboard  bool
	Explain          bool
	KeepEmpty        bool
	IncludeGenerated bool
	IncludeMinified  bool
	IncludeArtifacts bool
	ExtensionsRaw    string
	Extensions       []string
	SummaryOnly      bool
	NoTree           bool
	NoContents       bool
}
