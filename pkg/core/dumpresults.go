package core

import "time"

// DumpResults lists results of the dump.
type DumpResults struct {
	Start     time.Time
	End       time.Time
	Time      time.Time
	Timestamp string
	DumpStart time.Time
	DumpEnd   time.Time
	Uploads   []UploadResult
}

// UploadResult lists results of an individual upload
type UploadResult struct {
	Target string
	Filename string
	Start time.Time
	End time.Time
}
