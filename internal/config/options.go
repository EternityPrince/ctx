package config

type Options struct {
	Root            string
	IncludeHidden   bool
	MaxFileSize     int64
	OutputPath      string
	CopyToClipboard bool
	ExtensionsRaw   string
	Extensions      []string
	SummaryOnly     bool
	NoTree          bool
	NoContents      bool
}
