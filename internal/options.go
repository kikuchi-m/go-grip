package internal

// Options holds the settings the server is built from.
type Options struct {
	Host        string
	Port        int
	BoundingBox bool
	Browser     bool
	// Reload enables the automatic browser reload on file changes.
	Reload bool
	// Exclude holds name patterns hidden from the listing, direct access and the watcher.
	Exclude []string
}
