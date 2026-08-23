package backup

import "time"

type EndpointType string

const (
	EndpointLocal EndpointType = "local"
	EndpointMount EndpointType = "mount"
	EndpointSSH   EndpointType = "ssh"
)

type BackupMode string

const (
	ModeVersioned BackupMode = "versioned"
	ModeMirror    BackupMode = "mirror"
)

type TaskStatus string

const (
	StatusIdle    TaskStatus = "idle"
	StatusQueued  TaskStatus = "queued"
	StatusRunning TaskStatus = "running"
	StatusSuccess TaskStatus = "success"
	StatusFailed  TaskStatus = "failed"
)

type RunKind string

const (
	RunBackup  RunKind = "backup"
	RunRestore RunKind = "restore"
)

type Endpoint struct {
	Type         EndpointType `json:"type"`
	Path         string       `json:"path"`
	Host         string       `json:"host,omitempty"`
	Port         int          `json:"port,omitempty"`
	IdentityFile string       `json:"identityFile,omitempty"`
}

type Schedule struct {
	Kind    string `json:"kind"`
	Cron    string `json:"cron,omitempty"`
	Weekday int    `json:"weekday,omitempty"`
	Hour    int    `json:"hour,omitempty"`
	Minute  int    `json:"minute,omitempty"`
}

type RetentionPolicy struct {
	KeepLast int `json:"keepLast"`
}

type Task struct {
	ID          string          `json:"id"`
	Name        string          `json:"name"`
	Source      Endpoint        `json:"source"`
	Target      Endpoint        `json:"target"`
	Schedule    Schedule        `json:"schedule"`
	Excludes    []string        `json:"excludes,omitempty"`
	Retention   RetentionPolicy `json:"retention"`
	Mode        BackupMode      `json:"mode"`
	Incremental bool            `json:"incremental"`
	Delete      bool            `json:"delete"`
	Paused      bool            `json:"paused"`
	Status      TaskStatus      `json:"status"`
	LastResult  string          `json:"lastResult,omitempty"`
	LastRunAt   *time.Time      `json:"lastRunAt,omitempty"`
	NextRunAt   *time.Time      `json:"nextRunAt,omitempty"`
	CreatedAt   time.Time       `json:"createdAt"`
	UpdatedAt   time.Time       `json:"updatedAt"`
}

type History struct {
	ID               string     `json:"id"`
	TaskID           string     `json:"taskId"`
	Kind             RunKind    `json:"kind"`
	Status           TaskStatus `json:"status"`
	Phase            string     `json:"phase"`
	Version          string     `json:"version,omitempty"`
	RestoreTarget    string     `json:"restoreTarget,omitempty"`
	StartedAt        time.Time  `json:"startedAt"`
	FinishedAt       *time.Time `json:"finishedAt,omitempty"`
	TransferredBytes int64      `json:"transferredBytes"`
	Error            string     `json:"error,omitempty"`
	Log              string     `json:"log,omitempty"`
}

type PreflightResult struct {
	OK             bool    `json:"ok"`
	Checks         []Check `json:"checks"`
	EstimatedBytes int64   `json:"estimatedBytes"`
	AvailableBytes int64   `json:"availableBytes"`
}

type Check struct {
	Name    string `json:"name"`
	OK      bool   `json:"ok"`
	Message string `json:"message"`
}

type RestoreRequest struct {
	Version      string `json:"version"`
	Destination  string `json:"destination,omitempty"`
	Confirm      bool   `json:"confirm,omitempty"`
	PreviewToken string `json:"previewToken,omitempty"`
}

type RestorePreview struct {
	TaskID      string   `json:"taskId"`
	Version     string   `json:"version"`
	Destination string   `json:"destination"`
	Conflicts   []string `json:"conflicts"`
	Changes     []string `json:"changes"`
	Token       string   `json:"token"`
}

type state struct {
	Tasks     []Task    `json:"tasks"`
	Histories []History `json:"histories"`
}
