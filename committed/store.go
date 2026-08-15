package committed

import "context"

type Store interface {
	// Pipelines
	CreatePipeline(ctx context.Context, p *Pipeline) (int64, error)
	DeletePipeline(ctx context.Context, id int64) error
	GetPipeline(ctx context.Context, id int64) (*Pipeline, error)
	ListPipelines(ctx context.Context) ([]Pipeline, error)

	// Provider configs (global, shared across pipelines)
	GetProviderConfig(ctx context.Context, provider string) (string, error)
	SetProviderConfig(ctx context.Context, provider, configJSON string) error
	ListProviderConfigs(ctx context.Context) ([]ProviderConfig, error)

	// Bangous (aggregate root)
	CreateBangou(ctx context.Context, b *Bangou) (int64, error)
	GetBangou(ctx context.Context, id int64) (*Bangou, error)
	GetBangouByPipelineAndNumber(ctx context.Context, pipelineID int64, number string) (*Bangou, error)
	UpdateBangouPaths(ctx context.Context, id int64, nfoPath, coverPath, rawPath string) error
	DeleteBangou(ctx context.Context, id int64) error
	ListBangousByPipeline(ctx context.Context, pipelineID int64, limit, offset int, sort, order, status, q string) ([]Bangou, int, error)
	ListAllBangous(ctx context.Context) ([]Bangou, error)
	IsBangouCommitted(ctx context.Context, pipelineID int64, number string) (bool, error)

	// Bangou files
	CreateBangouFile(ctx context.Context, f *BangouFile) error
	GetBangouFileByID(ctx context.Context, id int64) (*BangouFile, error)
	DeleteBangouFile(ctx context.Context, id int64) error
	ListBangouFilesByBangou(ctx context.Context, bangouID int64) ([]BangouFile, error)
	ListAllBangouFiles(ctx context.Context) ([]BangouFile, error)
	SetBangouFileAlive(ctx context.Context, id int64, alive bool) error
	SetBangouFileLinkType(ctx context.Context, id int64, linkType string) error
	SetBangouFileSrcPath(ctx context.Context, id int64, srcPath string) error
	SetBangouFileMedia(ctx context.Context, id int64, fileSize int64, resolution, videoCodec, audioCodec, duration, bitrate string) error
	ListOrphanedBangouFiles(ctx context.Context) ([]BangouFile, error)
	CountBangouFiles(ctx context.Context, bangouID int64) (int, error)

	// Metadata (1:1 with Bangou)
	UpsertMetadata(ctx context.Context, m *Metadata) error
	GetMetadataByBangou(ctx context.Context, bangouID int64) (*Metadata, error)

	// Merged parts
	RecordMergedParts(ctx context.Context, parts []MergedPart) error
	GetMergedParts(ctx context.Context, number string) ([]MergedPart, error)

	// Legacy settings (kept for migration, minimal use going forward)
	GetSetting(ctx context.Context, key string) (string, error)
	SetSetting(ctx context.Context, key, value string) error
	GetAllSettings(ctx context.Context) (map[string]string, error)

	Close() error
}
