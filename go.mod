module github.com/sergklm98/kopia-server

go 1.27.1

require github.com/sergklm98/kopia-lib v0.0.0

require (
	github.com/klauspost/compress v1.20.0 // indirect
	github.com/klauspost/pgzip v1.2.6 // indirect
	github.com/pkg/errors v0.9.1 // indirect
	go.uber.org/multierr v1.11.0 // indirect
	go.uber.org/zap v1.28.0 // indirect
)

replace github.com/sergklm98/kopia-lib => ../kopia-lib
