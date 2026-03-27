package storage

type SymbolMatch struct {
	SymbolKey          string
	QName              string
	PackageImportPath  string
	FilePath           string
	Name               string
	Kind               string
	Receiver           string
	Signature          string
	Doc                string
	Line               int
	Column             int
	SearchKind         string
	SearchScore        int
	CallerCount        int
	CalleeCount        int
	ReferenceCount     int
	TestCount          int
	ReversePackageDeps int
	PackageImportance  int
	RelevanceScore     int
}

type RelatedSymbolView struct {
	Symbol      SymbolMatch
	UseFilePath string
	UseLine     int
	UseColumn   int
	Relation    string
	Why         string
}

type RefView struct {
	Symbol      SymbolMatch
	UseFilePath string
	UseLine     int
	UseColumn   int
	Kind        string
	Why         string
}

type TestView struct {
	TestKey           string
	PackageImportPath string
	Name              string
	FilePath          string
	Kind              string
	Line              int
	LinkKind          string
	Confidence        string
	Relation          string
	Why               string
	Score             int
}

type PackageSummary struct {
	ImportPath  string
	Name        string
	DirPath     string
	FileCount   int
	SymbolCount int
	TestCount   int
	LocalDeps   []string
	ReverseDeps []string
}

type ProvenanceItem struct {
	Kind     string
	Label    string
	FilePath string
	Line     int
	Why      string
}

type SearchPackageMetrics struct {
	ImportPath      string
	SymbolCount     int
	TestCount       int
	LocalDepCount   int
	ReverseDepCount int
	ImportanceScore int
}

type PackageMatch struct {
	ImportPath  string
	Name        string
	DirPath     string
	SearchKind  string
	SearchScore int
}

type ImpactNode struct {
	Symbol SymbolMatch
	Depth  int
}

type ImpactPackageReason struct {
	PackageImportPath string
	Why               []string
}

type ImpactFileReason struct {
	FilePath string
	Why      []string
}

type ImpactView struct {
	Target            SymbolMatch
	Package           PackageSummary
	DirectCallers     []RelatedSymbolView
	TransitiveCallers []ImpactNode
	InboundRefs       []RefView
	ReferencePackages []string
	CallerPackages    []string
	BlastPackages     []string
	ReferencePackageReasons []ImpactPackageReason
	CallerPackageReasons    []ImpactPackageReason
	BlastPackageReasons     []ImpactPackageReason
	BlastFileReasons   []ImpactFileReason
	ExpansionWhy       []string
	BlastFiles        []string
	EmpiricalFiles    []CoChangeItem
	EmpiricalPackages []CoChangeItem
	Tests             []TestView
	RecentDelta       SymbolImpactDelta
	HasRecentDelta    bool
}

type SymbolView struct {
	Symbol        SymbolMatch
	Package       PackageSummary
	Callers       []RelatedSymbolView
	Callees       []RelatedSymbolView
	ReferencesIn  []RefView
	ReferencesOut []RefView
	Tests         []TestView
	Siblings      []SymbolMatch
	QualityScore  int
	QualityWhy    []string
}

type ChangedSymbol struct {
	QName                 string
	FromSignature         string
	ToSignature           string
	FromPackageImportPath string
	ToPackageImportPath   string
	FromFilePath          string
	ToFilePath            string
	FromLine              int
	ToLine                int
	ContractChanged       bool
	Moved                 bool
}

type ChangedPackage struct {
	ImportPath          string
	Status              string
	FromFileCount       int
	ToFileCount         int
	FromSymbolCount     int
	ToSymbolCount       int
	FromTestCount       int
	ToTestCount         int
	FromLocalDepCount   int
	ToLocalDepCount     int
	FromReverseDepCount int
	ToReverseDepCount   int
}

type CallEdgeChange struct {
	CallerSymbolKey string
	CallerQName     string
	CalleeSymbolKey string
	CalleeQName     string
	FilePath        string
	Line            int
	Dispatch        string
}

type RefChange struct {
	FromPackageImportPath string
	FromSymbolKey         string
	FromQName             string
	ToSymbolKey           string
	ToQName               string
	FilePath              string
	Line                  int
	Kind                  string
}

type TestLinkChange struct {
	TestKey               string
	TestPackageImportPath string
	TestName              string
	SymbolKey             string
	SymbolQName           string
	LinkKind              string
	Confidence            string
}

type PackageDepChange struct {
	FromPackageImportPath string
	ToPackageImportPath   string
}

type SymbolImpactDelta struct {
	QName             string
	PackageImportPath string
	FilePath          string
	Status            string
	ContractChanged   bool
	Moved             bool
	AddedCallers      int
	RemovedCallers    int
	AddedCallees      int
	RemovedCallees    int
	AddedRefsIn       int
	RemovedRefsIn     int
	AddedRefsOut      int
	RemovedRefsOut    int
	AddedTests        int
	RemovedTests      int
	BlastRadius       int
	Why               []string
}

type SymbolHistoryEvent struct {
	FromSnapshotID  int64
	ToSnapshot      SnapshotInfo
	Status          string
	ContractChanged bool
	Moved           bool
	AddedCalls      int
	RemovedCalls    int
	AddedRefs       int
	RemovedRefs     int
	AddedTests      int
	RemovedTests    int
}

type SymbolHistoryView struct {
	Symbol        SymbolMatch
	IntroducedIn  SnapshotInfo
	LastChangedIn SnapshotInfo
	Events        []SymbolHistoryEvent
}

type PackageHistoryEvent struct {
	FromSnapshotID   int64
	ToSnapshot       SnapshotInfo
	Status           string
	FileDelta        int
	SymbolDelta      int
	TestDelta        int
	AddedDeps        int
	RemovedDeps      int
	MovedSymbols     int
	ChangedContracts int
}

type PackageHistoryView struct {
	Package       PackageSummary
	IntroducedIn  SnapshotInfo
	LastChangedIn SnapshotInfo
	Events        []PackageHistoryEvent
}

type CoChangeItem struct {
	Label     string
	Count     int
	Frequency float64
}

type CoChangeView struct {
	Scope             string
	Anchor            string
	AnchorFile        string
	AnchorPackage     string
	AnchorChangeCount int
	Files             []CoChangeItem
	Packages          []CoChangeItem
}

type DiffView struct {
	FromSnapshotID     int64
	ToSnapshotID       int64
	AddedFiles         []string
	ChangedFiles       []string
	DeletedFiles       []string
	AddedSymbols       []SymbolMatch
	RemovedSymbols     []SymbolMatch
	ChangedSymbols     []ChangedSymbol
	ChangedPackages    []ChangedPackage
	AddedCalls         []CallEdgeChange
	RemovedCalls       []CallEdgeChange
	AddedRefs          []RefChange
	RemovedRefs        []RefChange
	AddedTestLinks     []TestLinkChange
	RemovedTestLinks   []TestLinkChange
	AddedPackageDeps   []PackageDepChange
	RemovedPackageDeps []PackageDepChange
	ImpactedSymbols    []SymbolImpactDelta
}
