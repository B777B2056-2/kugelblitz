package constants

type MultiModalType string

const (
	MultiModalTypeUnknown MultiModalType = "unknown"
	MultiModalTypeText    MultiModalType = "text"
	MultiModalTypePDF     MultiModalType = "pdf"
	MultiModalTypeWord    MultiModalType = "word"
	MultiModalTypeExcel   MultiModalType = "excel"
	MultiModalTypeImage   MultiModalType = "image"
	MultiModalTypeVideo   MultiModalType = "video"
	MultiModalTypeAudio   MultiModalType = "audio"
)

type RoleType string

const (
	RoleSystem    RoleType = "system"
	RoleUser      RoleType = "user"
	RoleTool      RoleType = "tool"
	RoleAssistant RoleType = "assistant"
)
