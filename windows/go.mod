module openplayer

go 1.24

require (
	github.com/jchv/go-webview2 v0.0.0
	golang.org/x/sys v0.30.0
)

require github.com/jchv/go-winloader v0.0.0 // indirect

// Vendored sources (see third_party/*/REVISION); builds work fully offline.
replace (
	github.com/jchv/go-webview2 => ./third_party/go-webview2
	github.com/jchv/go-winloader => ./third_party/go-winloader
	golang.org/x/sys => ./third_party/sys
)
