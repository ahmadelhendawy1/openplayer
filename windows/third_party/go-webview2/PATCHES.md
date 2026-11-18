# Local changes to go-webview2

- `pkg/edge/openplayer_events.go` (new file): `Chromium.OnFileNavigation`, which
  cancels `file://` navigations and blocks pop-up windows, reporting the file URI
  to the host. Used by OpenPlayer's side panels for drag-and-drop.
- `pkg/edge/openplayer_events.go`: `Chromium.PutAllowExternalDrop`, a call to
  `ICoreWebView2Controller4::put_AllowExternalDrop` (IID
  `{97d418d5-a426-4e49-a151-e1a10f327d9e}`, vtable slot 37). OpenPlayer turns
  WebView2's own drop handling off so the panel window receives `WM_DROPFILES`
  with every dropped file.
