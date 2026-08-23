package downloads

import "time"

// Status is the persisted lifecycle state of a download task.
type Status string

const (
	StatusWaiting     Status = "waiting"
	StatusDownloading Status = "downloading"
	StatusPaused      Status = "paused"
	StatusCompleted   Status = "completed"
	StatusError       Status = "error"
)

// Task describes both persisted task metadata and its latest runtime progress.
type Task struct {
	ID               string     `json:"id"`
	URL              string     `json:"url"`
	Name             string     `json:"name"`
	TargetDirectory  string     `json:"targetDirectory"`
	Destination      string     `json:"destination"`
	Status           Status     `json:"status"`
	DownloadedBytes  int64      `json:"downloadedBytes"`
	TotalBytes       int64      `json:"totalBytes"`
	SpeedBytesPerSec int64      `json:"speedBytesPerSec"`
	EstimatedSeconds int64      `json:"estimatedSeconds"`
	ResumeSupported  bool       `json:"resumeSupported"`
	Error            string     `json:"error,omitempty"`
	ETag             string     `json:"etag,omitempty"`
	LastModified     string     `json:"lastModified,omitempty"`
	CreatedAt        time.Time  `json:"createdAt"`
	UpdatedAt        time.Time  `json:"updatedAt"`
	StartedAt        *time.Time `json:"startedAt,omitempty"`
	CompletedAt      *time.Time `json:"completedAt,omitempty"`
}

// Counts contains list totals grouped by the UI-visible task states.
type Counts struct {
	All         int `json:"all"`
	Downloading int `json:"downloading"`
	Completed   int `json:"completed"`
	Paused      int `json:"paused"`
	Error       int `json:"error"`
	Waiting     int `json:"waiting"`
}

// Statistics contains aggregate transfer data for the download engine.
type Statistics struct {
	DownloadSpeedBytesPerSec int64 `json:"downloadSpeedBytesPerSec"`
	UploadSpeedBytesPerSec   int64 `json:"uploadSpeedBytesPerSec"`
	TotalDownloadedBytes     int64 `json:"totalDownloadedBytes"`
	TotalUploadedBytes       int64 `json:"totalUploadedBytes"`
}

// Snapshot is returned by the list endpoint so task data and counts are from
// the same locked engine snapshot.
type Snapshot struct {
	Tasks      []Task     `json:"tasks"`
	Counts     Counts     `json:"counts"`
	Statistics Statistics `json:"statistics"`
	RootDir    string     `json:"rootDirectory"`
}

// AddRequest is the protocol-neutral task creation input.
type AddRequest struct {
	URL             string
	TargetDirectory string
}
