package storage

type Storage interface {
	Push(target, source string) (int64, error)
	Pull(source, target string) (int64, error)
	Protocol() string
	URL() string
}
