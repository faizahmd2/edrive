package snapshot

import "time"

type Manifest struct {
	Format      int       `json:"format"`
	CreatedAt   time.Time `json:"created_at"`
	Tool        string    `json:"tool"`
	ToolVersion string    `json:"tool_version"`
	Source      string    `json:"source"`
	Files       []File    `json:"files"`
}

type File struct {
	Path   string `json:"path"`
	Size   int64  `json:"size"`
	SHA256 string `json:"sha256"`
	Mode   uint32 `json:"mode"`
}
