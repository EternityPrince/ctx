package model

import "time"

type Snapshot struct {
	Root        string
	GeneratedAt time.Time
	Directories []string
	Files       []File
	Decisions   []Decision
	Stats       Stats
}

type File struct {
	Name          string
	AbsolutePath  string
	RelativePath  string
	Extension     string
	SizeBytes     int64
	LineCount     int
	NonEmptyLines int
	Content       string
}

type Stats struct {
	DirectoriesScanned int
	DirectoriesSkipped int
	FilesScanned       int
	FilesIncluded      int
	FilesSkipped       int
	TotalBytes         int64
	TotalLines         int
	TotalNonEmptyLines int
	AverageLines       float64
	LargestFile        FileMetric
	TopFiles           []FileMetric
	Extensions         []ExtensionMetric
	SkipReasons        []NamedMetric
}

type Decision struct {
	Path     string
	Kind     string
	Included bool
	Reason   string
}

type FileMetric struct {
	Path      string
	SizeBytes int64
	Lines     int
}

type ExtensionMetric struct {
	Name      string
	Files     int
	SizeBytes int64
	Lines     int
}

type NamedMetric struct {
	Name  string
	Count int
}
