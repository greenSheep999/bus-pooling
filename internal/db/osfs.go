package db

import (
	"io/fs"
	"os"
)

// osDirFS 包一层是为了让 LoadMigrations 能统一处理 embed FS 和磁盘目录。
func osDirFS(dir string) fs.FS { return os.DirFS(dir) }
