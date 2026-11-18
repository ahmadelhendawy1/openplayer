package com.ai4edutech.openplayer;

import android.Manifest;
import android.annotation.SuppressLint;
import android.app.Activity;
import android.content.BroadcastReceiver;
import android.content.ClipData;
import android.content.ContentResolver;
import android.content.ContentUris;
import android.content.Context;
import android.content.Intent;
import android.content.IntentFilter;
import android.content.SharedPreferences;
import android.content.pm.ActivityInfo;
import android.content.pm.PackageInstaller;
import android.content.pm.PackageManager;
import android.database.Cursor;
import android.graphics.Color;
import android.media.AudioManager;
import android.net.Uri;
import android.os.Build;
import android.os.Bundle;
import android.os.Handler;
import android.os.Looper;
import android.provider.DocumentsContract;
import android.provider.MediaStore;
import android.provider.OpenableColumns;
import android.provider.Settings;
import android.util.Log;
import android.view.KeyEvent;
import android.view.SurfaceHolder;
import android.view.SurfaceView;
import android.view.View;
import android.view.ViewGroup;
import android.view.Window;
import android.view.WindowInsets;
import android.view.WindowInsetsController;
import android.view.WindowManager;
import android.webkit.ConsoleMessage;
import android.webkit.JavascriptInterface;
import android.webkit.WebChromeClient;
import android.webkit.WebResourceRequest;
import android.webkit.WebSettings;
import android.webkit.WebView;
import android.webkit.WebViewClient;
import android.widget.FrameLayout;
import android.widget.Toast;

import org.json.JSONArray;
import org.json.JSONObject;

import java.io.BufferedReader;
import java.io.File;
import java.io.InputStream;
import java.io.InputStreamReader;
import java.io.OutputStream;
import java.nio.charset.Charset;
import java.nio.charset.StandardCharsets;
import java.util.ArrayList;
import java.util.Locale;
import java.util.List;
import java.util.concurrent.ExecutorService;
import java.util.concurrent.Executors;

/**
 * The screen: the video surface at the back and the OpenPlayer interface
 * (assets/ui/index.html) on top. The page talks to this class through the
 * "OPA" bridge; this class calls back into the page through window.OP.
 */
public class MainActivity extends Activity implements PlayerService.Listener {
    static final int REQ_FILES = 11, REQ_FOLDER = 12, REQ_SAVE_LIST = 13, REQ_LOAD_LIST = 14, REQ_SUBS = 15, REQ_PERM = 20;
    static final String[] AUDIO_EXT = {"mp3", "m4a", "aac", "flac", "wav", "ogg", "oga", "opus", "wma", "amr", "mid", "midi", "3ga", "mka", "alac", "aiff", "aif"};
    static final String[] VIDEO_EXT = {"mp4", "m4v", "mkv", "webm", "3gp", "3g2", "avi", "mov", "ts", "m2ts", "mts", "flv", "wmv", "mpg", "mpeg", "ogv"};

    WebView web;
    SurfaceView video;
    FrameLayout root;
    final Handler main = new Handler(Looper.getMainLooper());
    final ExecutorService io = Executors.newSingleThreadExecutor();
    SharedPreferences prefs;
    boolean pageReady, statePending;
    Intent pendingIntent;
    Runnable afterPermission;
    // update
    Updater.Info upd;
    final boolean[] updCancel = {false};
    boolean updBusy;
    File updFile;

    @SuppressLint({"SetJavaScriptEnabled", "AddJavascriptInterface"})
    @Override protected void onCreate(Bundle b) {
        super.onCreate(b);
        prefs = getSharedPreferences("ui", MODE_PRIVATE);
        Window w = getWindow();
        w.setStatusBarColor(Color.TRANSPARENT);
        w.setNavigationBarColor(Color.TRANSPARENT);
        if (Build.VERSION.SDK_INT >= 28) w.getAttributes().layoutInDisplayCutoutMode = WindowManager.LayoutParams.LAYOUT_IN_DISPLAY_CUTOUT_MODE_SHORT_EDGES;
        if (Build.VERSION.SDK_INT >= 30) w.setDecorFitsSystemWindows(false);
        else w.getDecorView().setSystemUiVisibility(View.SYSTEM_UI_FLAG_LAYOUT_STABLE | View.SYSTEM_UI_FLAG_LAYOUT_FULLSCREEN | View.SYSTEM_UI_FLAG_LAYOUT_HIDE_NAVIGATION);
        w.clearFlags(WindowManager.LayoutParams.FLAG_FULLSCREEN);
        setVolumeControlStream(AudioManager.STREAM_MUSIC);

        root = new FrameLayout(this);
        root.setBackgroundColor(Color.BLACK);
        video = new SurfaceView(this);
        root.addView(video, new FrameLayout.LayoutParams(ViewGroup.LayoutParams.MATCH_PARENT, ViewGroup.LayoutParams.MATCH_PARENT, android.view.Gravity.CENTER));
        video.getHolder().addCallback(new SurfaceHolder.Callback() {
            @Override public void surfaceCreated(SurfaceHolder h) { PlayerService s = PlayerService.instance; if (s != null) s.setSurface(h.getSurface()); }
            @Override public void surfaceChanged(SurfaceHolder h, int f, int wd, int ht) {}
            @Override public void surfaceDestroyed(SurfaceHolder h) { PlayerService s = PlayerService.instance; if (s != null) s.setSurface(null); }
        });

        web = new WebView(this);
        web.setBackgroundColor(Color.TRANSPARENT);
        WebSettings ws = web.getSettings();
        ws.setJavaScriptEnabled(true);
        ws.setDomStorageEnabled(true);
        ws.setAllowFileAccess(true); // only our own bundled page is loaded
        ws.setAllowContentAccess(false);
        ws.setMediaPlaybackRequiresUserGesture(true);
        ws.setTextZoom(100);
        web.setOverScrollMode(View.OVER_SCROLL_NEVER);
        web.addJavascriptInterface(new Bridge(), "OPA");
        web.setWebViewClient(new WebViewClient() {
            @Override public boolean shouldOverrideUrlLoading(WebView v, WebResourceRequest r) {
                Uri u = r.getUrl();
                if ("https".equals(u.getScheme())) { openLink(u.toString()); return true; }
                return !"file".equals(u.getScheme());
            }
            @Override public void onPageFinished(WebView v, String url) { onPage(); }
        });
        web.setWebChromeClient(new WebChromeClient() {
            @Override public boolean onConsoleMessage(ConsoleMessage m) {
                Log.d(PlayerService.TAG, "ui: " + m.message() + " @" + m.lineNumber());
                return true;
            }
        });
        root.addView(web, new FrameLayout.LayoutParams(ViewGroup.LayoutParams.MATCH_PARENT, ViewGroup.LayoutParams.MATCH_PARENT));
        setContentView(root);
        root.setOnApplyWindowInsetsListener((v, insets) -> { pushInsets(insets); return insets; });

        startService(new Intent(this, PlayerService.class));
        pendingIntent = getIntent();
        web.loadUrl("file:///android_asset/ui/index.html");

        IntentFilter f = new IntentFilter(Updater.ACTION_INSTALLED);
        if (Build.VERSION.SDK_INT >= 33) registerReceiver(installStatus, f, Context.RECEIVER_NOT_EXPORTED);
        else registerReceiver(installStatus, f);
    }

    @Override protected void onNewIntent(Intent i) {
        super.onNewIntent(i);
        setIntent(i);
        if (pageReady) handleIntent(i); else pendingIntent = i;
    }

    @Override protected void onResume() {
        super.onResume();
        PlayerService s = PlayerService.instance;
        if (s != null) { s.listener = this; s.setSurface(video.getHolder().getSurface().isValid() ? video.getHolder().getSurface() : null); }
        pushState();
    }

    @Override protected void onPause() {
        PlayerService s = PlayerService.instance;
        if (s != null) {
            if (s.isPlaying() && "video".equals(cur(s)) && !prefs.getBoolean("bgvideo", true)) s.pause();
            s.save();
        }
        super.onPause();
    }

    @Override protected void onDestroy() {
        PlayerService s = PlayerService.instance;
        if (s != null && s.listener == this) { s.listener = null; s.setSurface(null); }
        try { unregisterReceiver(installStatus); } catch (Exception ignored) {}
        web.destroy();
        super.onDestroy();
    }

    String cur(PlayerService s) { return s.index >= 0 && s.index < s.list.size() ? s.list.get(s.index).kind : ""; }

    @Override public void onBackPressed() {
        if (!pageReady) { super.onBackPressed(); return; }
        web.evaluateJavascript("window.OP&&OP.back?OP.back():false", v -> {
            if (!"true".equals(v)) moveTaskToBack(true); // keep playing in the background
        });
    }

    @Override public boolean onKeyDown(int code, KeyEvent e) {
        PlayerService s = PlayerService.instance;
        if (s != null) {
            switch (code) {
                case KeyEvent.KEYCODE_MEDIA_PLAY_PAUSE: s.toggle(); return true;
                case KeyEvent.KEYCODE_MEDIA_NEXT: s.next(true); return true;
                case KeyEvent.KEYCODE_MEDIA_PREVIOUS: s.prev(); return true;
            }
        }
        return super.onKeyDown(code, e);
    }

    // ---- page <-> app -------------------------------------------------------------

    void onPage() {
        pageReady = true;
        PlayerService s = PlayerService.instance;
        if (s != null) { s.listener = this; s.prepareRemembered(prefs.getBoolean("resume", true)); }
        js("OP.boot(" + JSONObject.quote(bootJson().toString()) + ")");
        pushInsets(root.getRootWindowInsets());
        pushState();
        if (pendingIntent != null) { handleIntent(pendingIntent); pendingIntent = null; }
        if (Build.VERSION.SDK_INT >= 33 && checkSelfPermission(Manifest.permission.POST_NOTIFICATIONS) != PackageManager.PERMISSION_GRANTED
                && !prefs.getBoolean("asked_notify", false)) {
            prefs.edit().putBoolean("asked_notify", true).apply();
            requestPermissions(new String[]{Manifest.permission.POST_NOTIFICATIONS}, REQ_PERM + 1);
        }
        if (prefs.getBoolean("check_app", true)) main.postDelayed(() -> checkUpdate(false), 5000);
    }

    JSONObject bootJson() {
        JSONObject o = new JSONObject();
        try {
            String lang = prefs.getString("lang", "");
            if (lang.isEmpty()) lang = Locale.getDefault().getLanguage().equals("ar") ? "ar" : "en";
            PlayerService s = PlayerService.instance;
            o.put("version", BuildInfo.VERSION).put("lang", lang).put("theme", prefs.getString("theme", "black-gold"))
                    .put("prefs", new JSONObject(prefs.getString("uiprefs", "{}")))
                    .put("fx", s != null ? s.fxJson() : new JSONObject())
                    .put("sdk", Build.VERSION.SDK_INT);
        } catch (Exception ignored) {}
        return o;
    }

    void js(String code) { main.post(() -> { if (pageReady) web.evaluateJavascript("try{" + code + "}catch(e){console.log(e)}", null); }); }

    @Override public void onPlayerChanged() {
        if (statePending) return;
        statePending = true;
        main.postDelayed(() -> { statePending = false; pushState(); }, 60);
    }

    void pushState() {
        PlayerService s = PlayerService.instance;
        if (s == null || !pageReady) return;
        boolean videoOn = s.videoW > 0 && s.videoH > 0 && "video".equals(cur(s));
        video.setVisibility(videoOn ? View.VISIBLE : View.INVISIBLE);
        if (videoOn) layoutVideo(s.videoW, s.videoH);
        if (s.isPlaying() && videoOn) getWindow().addFlags(WindowManager.LayoutParams.FLAG_KEEP_SCREEN_ON);
        else getWindow().clearFlags(WindowManager.LayoutParams.FLAG_KEEP_SCREEN_ON);
        js("OP.state(" + s.state() + ")");
    }

    @Override public void onVideoSize(int w, int h) { main.post(() -> { layoutVideo(w, h); pushState(); }); }

    @Override public void onPlayerError(String title) { js("OP.toast(" + JSONObject.quote("cannot_play|" + title) + ")"); }

    String zoom = "fit"; // fit | fill | stretch

    void layoutVideo(int vw, int vh) {
        if (vw <= 0 || vh <= 0) return;
        int W = root.getWidth(), H = root.getHeight();
        if (W == 0 || H == 0) { root.post(() -> layoutVideo(vw, vh)); return; }
        int w = W, h = H;
        float va = (float) vw / vh, sa = (float) W / H;
        if ("fit".equals(zoom)) { if (va > sa) h = Math.round(W / va); else w = Math.round(H * va); }
        else if ("fill".equals(zoom)) { if (va > sa) w = Math.round(H * va); else h = Math.round(W / va); }
        FrameLayout.LayoutParams lp = (FrameLayout.LayoutParams) video.getLayoutParams();
        if (lp.width != w || lp.height != h) { lp.width = w; lp.height = h; video.setLayoutParams(lp); }
    }

    void pushInsets(WindowInsets in) {
        if (in == null || !pageReady) return;
        float d = getResources().getDisplayMetrics().density;
        int t, bt, l, r;
        if (Build.VERSION.SDK_INT >= 30) {
            android.graphics.Insets i = in.getInsets(WindowInsets.Type.systemBars() | WindowInsets.Type.displayCutout());
            t = i.top; bt = i.bottom; l = i.left; r = i.right;
        } else {
            t = in.getSystemWindowInsetTop(); bt = in.getSystemWindowInsetBottom(); l = in.getSystemWindowInsetLeft(); r = in.getSystemWindowInsetRight();
        }
        js(String.format(Locale.US, "OP.insets(%.1f,%.1f,%.1f,%.1f)", t / d, r / d, bt / d, l / d));
    }

    void immersive(boolean on) {
        Window w = getWindow();
        if (Build.VERSION.SDK_INT >= 30) {
            WindowInsetsController c = w.getInsetsController();
            if (c == null) return;
            if (on) { c.hide(WindowInsets.Type.systemBars()); c.setSystemBarsBehavior(WindowInsetsController.BEHAVIOR_SHOW_TRANSIENT_BARS_BY_SWIPE); }
            else c.show(WindowInsets.Type.systemBars());
        } else {
            int f = View.SYSTEM_UI_FLAG_LAYOUT_STABLE | View.SYSTEM_UI_FLAG_LAYOUT_FULLSCREEN | View.SYSTEM_UI_FLAG_LAYOUT_HIDE_NAVIGATION;
            if (on) f |= View.SYSTEM_UI_FLAG_FULLSCREEN | View.SYSTEM_UI_FLAG_HIDE_NAVIGATION | View.SYSTEM_UI_FLAG_IMMERSIVE_STICKY;
            w.getDecorView().setSystemUiVisibility(f);
        }
    }

    void lightBars(boolean light) {
        if (Build.VERSION.SDK_INT >= 30) {
            WindowInsetsController c = getWindow().getInsetsController();
            if (c != null) {
                int m = WindowInsetsController.APPEARANCE_LIGHT_STATUS_BARS | WindowInsetsController.APPEARANCE_LIGHT_NAVIGATION_BARS;
                c.setSystemBarsAppearance(light ? m : 0, m);
            }
        } else {
            View d = getWindow().getDecorView();
            int f = d.getSystemUiVisibility();
            f = light ? (f | View.SYSTEM_UI_FLAG_LIGHT_STATUS_BAR) : (f & ~View.SYSTEM_UI_FLAG_LIGHT_STATUS_BAR);
            d.setSystemUiVisibility(f);
        }
    }

    // ---- opening files ---------------------------------------------------------------

    void handleIntent(Intent i) {
        if (i == null) return;
        List<Uri> uris = new ArrayList<>();
        String a = i.getAction();
        if (Intent.ACTION_VIEW.equals(a) && i.getData() != null) uris.add(i.getData());
        else if (Intent.ACTION_SEND.equals(a)) { Uri u = i.getParcelableExtra(Intent.EXTRA_STREAM); if (u != null) uris.add(u); }
        else if (Intent.ACTION_SEND_MULTIPLE.equals(a)) { ArrayList<Uri> l = i.getParcelableArrayListExtra(Intent.EXTRA_STREAM); if (l != null) uris.addAll(l); }
        if (uris.isEmpty()) return;
        i.setAction(Intent.ACTION_MAIN); // do not open again after rotation
        openUris(uris, true, false);
    }

    void openUris(List<Uri> uris, boolean playNow, boolean persist) {
        io.execute(() -> {
            List<PlayerService.Item> items = new ArrayList<>();
            for (Uri u : uris) {
                if (persist) { try { getContentResolver().takePersistableUriPermission(u, Intent.FLAG_GRANT_READ_URI_PERMISSION); } catch (Exception ignored) {} }
                String name = displayName(u);
                String kind = kindOf(u, name);
                if (kind == null) continue;
                items.add(new PlayerService.Item(u.toString(), stripExt(name), kind));
            }
            main.post(() -> {
                PlayerService s = PlayerService.instance;
                if (s == null || items.isEmpty()) { js("OP.toast('nothing_to_play')"); return; }
                s.add(items, playNow);
                js("OP.added(" + items.size() + ")");
            });
        });
    }

    String displayName(Uri u) {
        if ("content".equals(u.getScheme())) {
            try (Cursor c = getContentResolver().query(u, new String[]{OpenableColumns.DISPLAY_NAME}, null, null, null)) {
                if (c != null && c.moveToFirst() && c.getString(0) != null) return c.getString(0);
            } catch (Exception ignored) {}
        }
        String p = u.getLastPathSegment();
        if (p == null) return u.toString();
        int k = p.lastIndexOf('/');
        return k >= 0 ? p.substring(k + 1) : p;
    }

    String kindOf(Uri u, String name) {
        String t = null;
        try { t = getContentResolver().getType(u); } catch (Exception ignored) {}
        if (t != null && t.startsWith("video/")) return "video";
        if (t != null && t.startsWith("audio/")) return "audio";
        String ext = ext(name);
        for (String e : VIDEO_EXT) if (e.equals(ext)) return "video";
        for (String e : AUDIO_EXT) if (e.equals(ext)) return "audio";
        if (t != null && (t.equals("application/ogg") || t.equals("application/x-mpegurl"))) return "audio";
        return null;
    }

    static String ext(String n) { int k = n.lastIndexOf('.'); return k >= 0 ? n.substring(k + 1).toLowerCase(Locale.US) : ""; }
    static String stripExt(String n) { int k = n.lastIndexOf('.'); return k > 0 ? n.substring(0, k) : n; }

    void pickFiles() {
        Intent i = new Intent(Intent.ACTION_OPEN_DOCUMENT).addCategory(Intent.CATEGORY_OPENABLE).setType("*/*")
                .putExtra(Intent.EXTRA_MIME_TYPES, new String[]{"audio/*", "video/*", "application/ogg"})
                .putExtra(Intent.EXTRA_ALLOW_MULTIPLE, true);
        start(i, REQ_FILES);
    }

    void pickFolder() { start(new Intent(Intent.ACTION_OPEN_DOCUMENT_TREE), REQ_FOLDER); }

    void start(Intent i, int code) {
        try { startActivityForResult(i, code); } catch (Exception e) { js("OP.toast('no_picker')"); }
    }

    @Override protected void onActivityResult(int req, int res, Intent data) {
        super.onActivityResult(req, res, data);
        if (res != RESULT_OK || data == null) return;
        switch (req) {
            case REQ_FILES: {
                List<Uri> l = new ArrayList<>();
                ClipData cd = data.getClipData();
                if (cd != null) for (int k = 0; k < cd.getItemCount(); k++) l.add(cd.getItemAt(k).getUri());
                else if (data.getData() != null) l.add(data.getData());
                openUris(l, pendingPlayNow, true);
                break;
            }
            case REQ_FOLDER: {
                Uri tree = data.getData();
                if (tree == null) return;
                try { getContentResolver().takePersistableUriPermission(tree, Intent.FLAG_GRANT_READ_URI_PERMISSION); } catch (Exception ignored) {}
                io.execute(() -> {
                    List<Uri> l = new ArrayList<>();
                    walk(tree, DocumentsContract.getTreeDocumentId(tree), l, 0);
                    main.post(() -> { if (l.isEmpty()) js("OP.toast('nothing_to_play')"); else openUris(l, pendingPlayNow, false); });
                });
                break;
            }
            case REQ_SAVE_LIST: savePlaylist(data.getData()); break;
            case REQ_LOAD_LIST: loadPlaylist(data.getData()); break;
            case REQ_SUBS: loadSubs(data.getData()); break;
        }
    }

    boolean pendingPlayNow = true;

    void walk(Uri tree, String docId, List<Uri> out, int depth) {
        if (depth > 6 || out.size() > 5000) return;
        Uri kids = DocumentsContract.buildChildDocumentsUriUsingTree(tree, docId);
        List<String[]> found = new ArrayList<>();
        try (Cursor c = getContentResolver().query(kids, new String[]{DocumentsContract.Document.COLUMN_DOCUMENT_ID,
                DocumentsContract.Document.COLUMN_DISPLAY_NAME, DocumentsContract.Document.COLUMN_MIME_TYPE}, null, null, null)) {
            while (c != null && c.moveToNext()) found.add(new String[]{c.getString(0), c.getString(1), c.getString(2)});
        } catch (Exception e) { return; }
        found.sort((x, y) -> String.CASE_INSENSITIVE_ORDER.compare(x[1] == null ? "" : x[1], y[1] == null ? "" : y[1]));
        for (String[] f : found) {
            if (DocumentsContract.Document.MIME_TYPE_DIR.equals(f[2])) walk(tree, f[0], out, depth + 1);
            else {
                String n = f[1] == null ? "" : f[1], t = f[2] == null ? "" : f[2], e = ext(n);
                boolean media = t.startsWith("audio/") || t.startsWith("video/");
                if (!media) { for (String x : AUDIO_EXT) if (x.equals(e)) media = true; for (String x : VIDEO_EXT) if (x.equals(e)) media = true; }
                if (media) out.add(DocumentsContract.buildDocumentUriUsingTree(tree, f[0]));
            }
        }
    }

    // ---- library (all songs / videos on the phone) ------------------------------------

    void library(String kind) {
        String perm = Build.VERSION.SDK_INT >= 33
                ? ("video".equals(kind) ? Manifest.permission.READ_MEDIA_VIDEO : Manifest.permission.READ_MEDIA_AUDIO)
                : Manifest.permission.READ_EXTERNAL_STORAGE;
        if (checkSelfPermission(perm) != PackageManager.PERMISSION_GRANTED) {
            afterPermission = () -> library(kind);
            String[] ask = Build.VERSION.SDK_INT >= 33
                    ? new String[]{Manifest.permission.READ_MEDIA_AUDIO, Manifest.permission.READ_MEDIA_VIDEO}
                    : new String[]{Manifest.permission.READ_EXTERNAL_STORAGE};
            requestPermissions(ask, REQ_PERM);
            return;
        }
        io.execute(() -> {
            JSONArray a = new JSONArray();
            boolean vid = "video".equals(kind);
            Uri base = vid ? MediaStore.Video.Media.EXTERNAL_CONTENT_URI : MediaStore.Audio.Media.EXTERNAL_CONTENT_URI;
            List<String> cols = new ArrayList<>();
            cols.add(MediaStore.MediaColumns._ID); cols.add(MediaStore.MediaColumns.DISPLAY_NAME); cols.add(MediaStore.MediaColumns.TITLE);
            cols.add(MediaStore.MediaColumns.DURATION); cols.add(MediaStore.MediaColumns.DATE_ADDED); cols.add(MediaStore.MediaColumns.SIZE);
            if (!vid) { cols.add(MediaStore.Audio.AudioColumns.ARTIST); cols.add(MediaStore.Audio.AudioColumns.ALBUM); }
            cols.add(Build.VERSION.SDK_INT >= 29 ? MediaStore.MediaColumns.RELATIVE_PATH : MediaStore.MediaColumns.DATA);
            String sel = vid ? null : MediaStore.Audio.AudioColumns.IS_MUSIC + "!=0 OR " + MediaStore.MediaColumns.DURATION + ">60000";
            try (Cursor c = getContentResolver().query(base, cols.toArray(new String[0]), sel, null, MediaStore.MediaColumns.DATE_ADDED + " DESC")) {
                while (c != null && c.moveToNext()) {
                    long id = c.getLong(0);
                    String name = c.getString(1), title = c.getString(2);
                    if (title == null || title.isEmpty()) title = stripExt(name == null ? "?" : name);
                    String folder = c.getString(c.getColumnCount() - 1);
                    if (folder != null) {
                        if (Build.VERSION.SDK_INT < 29) { int k = folder.lastIndexOf('/'); folder = k > 0 ? folder.substring(0, k) : folder; }
                        folder = folder.replaceAll("/+$", "");
                        int k = folder.lastIndexOf('/');
                        folder = k >= 0 ? folder.substring(k + 1) : folder;
                    }
                    JSONObject o = new JSONObject().put("uri", ContentUris.withAppendedId(base, id).toString()).put("title", title)
                            .put("dur", c.getLong(3)).put("added", c.getLong(4)).put("size", c.getLong(5)).put("kind", vid ? "video" : "audio")
                            .put("folder", folder == null ? "" : folder);
                    if (!vid) {
                        String ar = c.getString(6);
                        o.put("artist", ar == null || "<unknown>".equals(ar) ? "" : ar).put("album", c.getString(7) == null ? "" : c.getString(7));
                    }
                    a.put(o);
                }
            } catch (Exception e) { Log.w(PlayerService.TAG, "library", e); }
            js("OP.library(" + JSONObject.quote(kind) + "," + a + ")");
        });
    }

    @Override public void onRequestPermissionsResult(int req, String[] p, int[] r) {
        super.onRequestPermissionsResult(req, p, r);
        if (req != REQ_PERM) return;
        boolean any = false;
        for (int x : r) any |= x == PackageManager.PERMISSION_GRANTED;
        Runnable next = afterPermission;
        afterPermission = null;
        if (any && next != null) next.run();
        else if (!any) js("OP.permissionDenied()");
    }

    // ---- playlists (.m3u) ------------------------------------------------------------

    void savePlaylist(Uri to) {
        PlayerService s = PlayerService.instance;
        if (to == null || s == null) return;
        StringBuilder sb = new StringBuilder("#EXTM3U\n");
        for (PlayerService.Item it : s.list) sb.append("#EXTINF:-1,").append(it.title.replace('\n', ' ')).append('\n').append(it.uri).append('\n');
        io.execute(() -> {
            try (OutputStream os = getContentResolver().openOutputStream(to, "wt")) {
                if (os == null) throw new Exception("no stream");
                os.write(sb.toString().getBytes(StandardCharsets.UTF_8));
                js("OP.toast('list_saved')");
            } catch (Exception e) { js("OP.toast('list_save_failed')"); }
        });
    }

    void loadPlaylist(Uri from) {
        if (from == null) return;
        io.execute(() -> {
            List<PlayerService.Item> items = new ArrayList<>();
            int skipped = 0;
            try (InputStream in = getContentResolver().openInputStream(from)) {
                if (in == null) throw new Exception("no stream");
                BufferedReader br = new BufferedReader(new InputStreamReader(in, StandardCharsets.UTF_8));
                String line, title = null;
                while ((line = br.readLine()) != null) {
                    line = line.replace("﻿", "").trim();
                    if (line.isEmpty()) continue;
                    if (line.startsWith("#EXTINF")) { int k = line.indexOf(','); title = k >= 0 ? line.substring(k + 1).trim() : null; continue; }
                    if (line.startsWith("#")) continue;
                    Uri u;
                    if (line.startsWith("content://") || line.startsWith("https://") || line.startsWith("http://")) u = Uri.parse(line);
                    else if (line.startsWith("file://")) u = Uri.parse(line);
                    else if (line.startsWith("/")) u = Uri.fromFile(new File(line));
                    else { skipped++; title = null; continue; } // a Windows path or a path relative to the list: not reachable here
                    String name = title != null && !title.isEmpty() ? title : stripExt(displayName(u));
                    String kind = kindOf(u, displayName(u));
                    items.add(new PlayerService.Item(u.toString(), name, kind == null ? "audio" : kind));
                    title = null;
                }
            } catch (Exception e) { js("OP.toast('list_load_failed')"); return; }
            final int sk = skipped;
            main.post(() -> {
                PlayerService s = PlayerService.instance;
                if (s == null) return;
                if (items.isEmpty()) { js("OP.toast('list_empty|" + sk + "')"); return; }
                s.replace(items, 0);
                js("OP.toast(" + JSONObject.quote("list_loaded|" + items.size() + "|" + sk) + ")");
            });
        });
    }

    // ---- subtitles ---------------------------------------------------------------------

    void loadSubs(Uri u) {
        if (u == null) return;
        io.execute(() -> {
            try (InputStream in = getContentResolver().openInputStream(u)) {
                if (in == null) throw new Exception("no stream");
                byte[] b = Updater.readAll(in, 8 << 20);
                String t = new String(b, StandardCharsets.UTF_8);
                if (t.indexOf('�') >= 0) t = new String(b, Charset.forName("windows-1256")); // older Arabic subtitle files
                js("OP.subs(" + JSONObject.quote(t) + "," + JSONObject.quote(displayName(u)) + ")");
            } catch (Exception e) { js("OP.toast('subs_failed')"); }
        });
    }

    // ---- updates ------------------------------------------------------------------------

    void checkUpdate(boolean manual) {
        io.execute(() -> {
            try {
                Updater.Info i = Updater.fetch();
                upd = i;
                boolean newer = Updater.isNewer(i, BuildInfo.VERSION, BuildInfo.CODE);
                if (!newer && !manual) return;
                if (newer && !manual && !i.force && i.version.equals(prefs.getString("remind_version", ""))
                        && System.currentTimeMillis() < prefs.getLong("remind_after", 0)) return;
                js("OP.update(" + i.json(BuildInfo.VERSION, BuildInfo.CODE, prefs.getString("lang", "en")) + ")");
            } catch (Exception e) {
                Updater.log("check", e);
                if (manual) js("OP.updateError(" + JSONObject.quote("check|" + e.getMessage()) + ")");
            }
        });
    }

    void startUpdate() {
        Updater.Info i = upd;
        if (i == null || updBusy) return;
        if (Build.VERSION.SDK_INT >= 26 && !getPackageManager().canRequestPackageInstalls()) {
            js("OP.updateError('allow_install')");
            try {
                startActivity(new Intent(Settings.ACTION_MANAGE_UNKNOWN_APP_SOURCES, Uri.parse("package:" + getPackageName())));
            } catch (Exception ignored) {}
            return;
        }
        updBusy = true; updCancel[0] = false;
        io.execute(() -> {
            try {
                File f = updFile != null && updFile.exists() && updFile.getName().contains(i.version) ? updFile
                        : Updater.download(this, i, (pct, got, total) -> js(String.format(Locale.US, "OP.updateProgress(%d,%.1f,%.1f)", pct, got / 1048576.0, Math.max(got, total) / 1048576.0)), updCancel);
                updFile = f;
                js("OP.updateProgress(100,-1,-1)");
                Updater.install(this, f);
            } catch (SecurityException e) {
                js("OP.updateError('hash')");
            } catch (InterruptedException e) {
                // cancelled by the user
            } catch (Exception e) {
                Updater.log("download", e);
                js("OP.updateError(" + JSONObject.quote("download|" + e.getMessage()) + ")");
            } finally { updBusy = false; }
        });
    }

    final BroadcastReceiver installStatus = new BroadcastReceiver() {
        @Override public void onReceive(Context c, Intent i) {
            int st = i.getIntExtra(PackageInstaller.EXTRA_STATUS, PackageInstaller.STATUS_FAILURE);
            if (st == PackageInstaller.STATUS_PENDING_USER_ACTION) {
                Intent confirm = i.getParcelableExtra(Intent.EXTRA_INTENT);
                if (confirm != null) { confirm.addFlags(Intent.FLAG_ACTIVITY_NEW_TASK); try { startActivity(confirm); } catch (Exception ignored) {} }
            } else if (st != PackageInstaller.STATUS_SUCCESS) {
                String m = i.getStringExtra(PackageInstaller.EXTRA_STATUS_MESSAGE);
                js("OP.updateError(" + JSONObject.quote("install|" + (m == null ? st : m)) + ")");
            }
        }
    };

    void openLink(String url) {
        try { startActivity(new Intent(Intent.ACTION_VIEW, Uri.parse(url))); } catch (Exception ignored) {}
    }

    // ---- bridge (runs on a WebView thread: hop to the main thread) -----------------------

    final class Bridge {
        @JavascriptInterface public void cmd(String json) { main.post(() -> run(json)); }
        @JavascriptInterface public String version() { return BuildInfo.VERSION; }
        @JavascriptInterface public void thumb(String uri, String kind) {
            Thumbs.getData(MainActivity.this, uri, kind, 160, d -> js("OP.thumb(" + JSONObject.quote(uri) + "," + JSONObject.quote(d) + ")"));
        }
        @JavascriptInterface public void art(String uri, String kind) {
            Thumbs.getData(MainActivity.this, uri, kind, 640, d -> js("OP.art(" + JSONObject.quote(uri) + "," + JSONObject.quote(d) + ")"));
        }
        @JavascriptInterface public int volume() { AudioManager am = (AudioManager) getSystemService(AUDIO_SERVICE); return am.getStreamVolume(AudioManager.STREAM_MUSIC) * 100 / Math.max(1, am.getStreamMaxVolume(AudioManager.STREAM_MUSIC)); }
    }

    void run(String json) {
        PlayerService s = PlayerService.instance;
        try {
            JSONObject o = new JSONObject(json);
            String a = o.optString("a");
            switch (a) {
                case "play": if (s != null) s.play(o.optInt("i")); break;
                case "toggle": if (s != null) s.toggle(); break;
                case "next": if (s != null) s.next(true); break;
                case "prev": if (s != null) s.prev(); break;
                case "seek": if (s != null) s.seek(o.optInt("ms")); break;
                case "seekBy": if (s != null) s.seek(s.position() + o.optInt("ms")); break;
                case "speed": if (s != null) s.setSpeed((float) o.optDouble("v", 1)); break;
                case "repeat": if (s != null) { s.repeat = o.optString("v", "off"); s.save(); s.changed(); } break;
                case "shuffle": if (s != null) s.shuffle(); break;
                case "remove": if (s != null) s.remove(o.optInt("i")); break;
                case "move": if (s != null) s.move(o.optInt("from"), o.optInt("to")); break;
                case "clear": if (s != null) s.clear(); break;
                case "sleep": if (s != null) { int m = o.optInt("min"); s.sleepAt = m > 0 ? System.currentTimeMillis() + m * 60000L : 0; s.changed(); } break;
                case "track": if (s != null) s.selectTrack(o.optInt("id")); break;
                case "playList": {
                    if (s == null) break;
                    JSONArray l = o.optJSONArray("list");
                    List<PlayerService.Item> items = new ArrayList<>();
                    for (int k = 0; l != null && k < l.length(); k++) {
                        JSONObject it = l.getJSONObject(k);
                        items.add(new PlayerService.Item(it.getString("uri"), it.optString("title"), it.optString("kind", "audio")));
                    }
                    if (o.optBoolean("append")) s.add(items, false); else s.replace(items, o.optInt("start"));
                    break;
                }
                case "fx": {
                    if (s == null) break;
                    JSONArray b = o.optJSONArray("bands");
                    double[] bands = new double[10];
                    for (int k = 0; b != null && k < 10 && k < b.length(); k++) bands[k] = b.optDouble(k, 0);
                    s.setFx(o.optBoolean("eq"), o.optDouble("pre", 0), bands, o.optBoolean("norm"), o.optInt("amp", 100));
                    break;
                }
                case "openFiles": pendingPlayNow = !o.optBoolean("append"); pickFiles(); break;
                case "openFolder": pendingPlayNow = !o.optBoolean("append"); pickFolder(); break;
                case "library": library(o.optString("kind", "audio")); break;
                case "saveList":
                    start(new Intent(Intent.ACTION_CREATE_DOCUMENT).addCategory(Intent.CATEGORY_OPENABLE).setType("audio/x-mpegurl")
                            .putExtra(Intent.EXTRA_TITLE, "OpenPlayer playlist.m3u8"), REQ_SAVE_LIST);
                    break;
                case "loadList":
                    start(new Intent(Intent.ACTION_OPEN_DOCUMENT).addCategory(Intent.CATEGORY_OPENABLE).setType("*/*")
                            .putExtra(Intent.EXTRA_MIME_TYPES, new String[]{"audio/x-mpegurl", "audio/mpegurl", "application/vnd.apple.mpegurl", "application/x-mpegurl", "text/plain", "application/octet-stream"}), REQ_LOAD_LIST);
                    break;
                case "subs":
                    start(new Intent(Intent.ACTION_OPEN_DOCUMENT).addCategory(Intent.CATEGORY_OPENABLE).setType("*/*"), REQ_SUBS);
                    break;
                case "pref": {
                    SharedPreferences.Editor e = prefs.edit();
                    if (o.has("lang")) e.putString("lang", o.getString("lang"));
                    if (o.has("theme")) e.putString("theme", o.getString("theme"));
                    if (o.has("uiprefs")) {
                        JSONObject up = o.getJSONObject("uiprefs");
                        e.putString("uiprefs", up.toString());
                        e.putBoolean("check_app", up.optBoolean("checkApp", true));
                        e.putBoolean("bgvideo", up.optBoolean("bgVideo", true));
                        e.putBoolean("resume", up.optBoolean("resume", true));
                    }
                    e.apply();
                    break;
                }
                case "lightBars": lightBars(o.optBoolean("v")); break;
                case "immersive": immersive(o.optBoolean("v")); break;
                case "rotate": {
                    String v = o.optString("v");
                    setRequestedOrientation("landscape".equals(v) ? ActivityInfo.SCREEN_ORIENTATION_SENSOR_LANDSCAPE
                            : "portrait".equals(v) ? ActivityInfo.SCREEN_ORIENTATION_SENSOR_PORTRAIT : ActivityInfo.SCREEN_ORIENTATION_UNSPECIFIED);
                    break;
                }
                case "zoom": zoom = o.optString("v", "fit"); if (s != null) layoutVideo(s.videoW, s.videoH); break;
                case "brightness": {
                    WindowManager.LayoutParams lp = getWindow().getAttributes();
                    double v = o.optDouble("v", -1);
                    lp.screenBrightness = v < 0 ? WindowManager.LayoutParams.BRIGHTNESS_OVERRIDE_NONE : (float) Math.max(0.02, Math.min(1, v));
                    getWindow().setAttributes(lp);
                    break;
                }
                case "volume": {
                    AudioManager am = (AudioManager) getSystemService(AUDIO_SERVICE);
                    int max = am.getStreamMaxVolume(AudioManager.STREAM_MUSIC);
                    am.setStreamVolume(AudioManager.STREAM_MUSIC, (int) Math.round(Math.max(0, Math.min(1, o.optDouble("v"))) * max), 0);
                    break;
                }
                case "checkUpdate": checkUpdate(true); break;
                case "update": startUpdate(); break;
                case "updateLater":
                    updCancel[0] = true;
                    if (upd != null && !upd.force) prefs.edit().putString("remind_version", upd.version).putLong("remind_after", System.currentTimeMillis() + 86400000L).apply();
                    break;
                case "site": openLink(upd != null && upd.url.startsWith("https://") ? upd.url : Updater.SITE); break;
                case "toast": Toast.makeText(this, o.optString("v"), Toast.LENGTH_SHORT).show(); break;
                case "quit": if (s != null) { s.pause(); s.stopFg(true); s.stopSelf(); } finishAndRemoveTask(); break;
                case "background": moveTaskToBack(true); break;
            }
        } catch (Exception e) { Log.w(PlayerService.TAG, "cmd " + json, e); }
    }

    @SuppressWarnings("unused") static ContentResolver cr(Context c) { return c.getContentResolver(); }
}
