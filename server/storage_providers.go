package server

import (
	_ "github.com/sergklm98/kopia-lib/lib/repo/blob/azure"
	_ "github.com/sergklm98/kopia-lib/lib/repo/blob/b2"
	_ "github.com/sergklm98/kopia-lib/lib/repo/blob/filesystem"
	_ "github.com/sergklm98/kopia-lib/lib/repo/blob/gcs"
	_ "github.com/sergklm98/kopia-lib/lib/repo/blob/gdrive"
	_ "github.com/sergklm98/kopia-lib/lib/repo/blob/rclone"
	_ "github.com/sergklm98/kopia-lib/lib/repo/blob/s3"
	_ "github.com/sergklm98/kopia-lib/lib/repo/blob/sftp"
	_ "github.com/sergklm98/kopia-lib/lib/repo/blob/webdav"
)
