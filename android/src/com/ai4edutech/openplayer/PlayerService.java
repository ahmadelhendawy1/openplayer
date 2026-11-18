package com.ai4edutech.openplayer;

import android.app.Notification;
import android.app.NotificationChannel;
import android.app.NotificationManager;
import android.app.PendingIntent;
import android.app.Service;
import android.content.BroadcastReceiver;
import android.content.Context;
import android.content.Intent;
import android.content.IntentFilter;
import android.content.SharedPreferences;
import android.content.pm.ServiceInfo;
import android.graphics.Bitmap;
import android.media.AudioAttributes;
import android.media.AudioFocusRequest;
import android.media.AudioManager;
import android.media.MediaMetadata;
import android.media.MediaPlayer;
import android.media.PlaybackParams;
import android.media.audiofx.DynamicsProcessing;
import android.media.audiofx.Equalizer;
import android.media.audiofx.LoudnessEnhancer;
import android.media.session.MediaSession;
import android.media.session.PlaybackState;
import android.net.Uri;
import android.os.Build;
import android.os.Handler;
import android.os.IBinder;
import android.os.Looper;
import android.os.PowerManager;
import android.util.Log;
import android.view.Surface;

import org.json.JSONArray;
import org.json.JSONObject;

import java.util.ArrayList;
import java.util.Collections;
import java.util.List;
import java.util.Random;

/**
 * Playback: Android's built-in media engine (MediaPlayer), the playlist,
 * audio effects (equalizer, normalize, amplifier up to 500 %), the media
 * session (lock screen, headset buttons, Bluetooth) and the notification that
 * keeps music playing in the background.
 */
public class PlayerService extends Service {
    static final String TAG = "OpenPlayer";
    static final String CH = "playback";
    static final int NOTE_ID = 7;
    static final String ACT_TOGGLE = "op.toggle", ACT_NEXT = "op.next", ACT_PREV = "op.prev", ACT_STOP = "op.stop";

    static PlayerService instance;

    public interface Listener { void onPlayerChanged(); void onVideoSize(int w, int h); void onPlayerError(String msg); }

    static class Item {
        String uri, title, kind; // kind: "audio" | "video"
        Item(String u, String t, String k) { uri = u; title = t; kind = k; }
        JSONObject json() throws Exception { return new JSONObject().put("uri", uri).put("title", title).put("kind", kind); }
    }

    final List<Item> list = new ArrayList<>();
    int index = -1;
    MediaPlayer mp;
    boolean prepared, wantPlay, resumeNext;
    int videoW, videoH;
    Surface surface;
    float speed = 1f;
    String repeat = "off"; // off | all | one
    long sleepAt = 0;
    Listener listener;
    MediaSession session;
    AudioManager am;
    AudioFocusRequest focusReq;
    boolean foreground, noisyRegistered;
    Bitmap art;
    final Handler main = new Handler(Looper.getMainLooper());
    SharedPreferences prefs;

    // audio effects
    Equalizer eq;
    LoudnessEnhancer le;
    DynamicsProcessing dyn;
    int fxSession = -1;
    boolean eqOn, normOn;
    double preamp;
    final double[] bands = new double[10];
    int amp = 100;
    static final int[] FREQS = {31, 62, 125, 250, 500, 1000, 2000, 4000, 8000, 16000};

    @Override public IBinder onBind(Intent i) { return null; }

    @Override public void onCreate() {
        super.onCreate();
        instance = this;
        prefs = getSharedPreferences("player", MODE_PRIVATE);
        am = (AudioManager) getSystemService(AUDIO_SERVICE);
        session = new MediaSession(this, "OpenPlayer");
        session.setCallback(new MediaSession.Callback() {
            @Override public void onPlay() { resume(); }
            @Override public void onPause() { pause(); }
            @Override public void onSkipToNext() { next(true); }
            @Override public void onSkipToPrevious() { prev(); }
            @Override public void onSeekTo(long pos) { seek((int) pos); }
            @Override public void onStop() { pause(); }
        });
        session.setActive(true);
        if (Build.VERSION.SDK_INT >= 26) {
            NotificationChannel c = new NotificationChannel(CH, "Playback", NotificationManager.IMPORTANCE_LOW);
            c.setShowBadge(false);
            ((NotificationManager) getSystemService(NOTIFICATION_SERVICE)).createNotificationChannel(c);
        }
        restore();
        main.postDelayed(tick, 1000);
    }

    @Override public int onStartCommand(Intent intent, int flags, int startId) {
        if (intent != null && intent.getAction() != null) {
            switch (intent.getAction()) {
                case ACT_TOGGLE: toggle(); break;
                case ACT_NEXT: next(true); break;
                case ACT_PREV: prev(); break;
                case ACT_STOP: pause(); stopFg(true); break;
            }
        }
        return START_NOT_STICKY;
    }

    @Override public void onTaskRemoved(Intent rootIntent) {
        if (!isPlaying()) { stopSelf(); }
    }

    @Override public void onDestroy() {
        save();
        releaseFx();
        if (mp != null) { mp.release(); mp = null; }
        session.release();
        if (noisyRegistered) { try { unregisterReceiver(noisy); } catch (Exception ignored) {} }
        instance = null;
        super.onDestroy();
    }

    // ---- playlist -----------------------------------------------------------

    void add(List<Item> items, boolean playNow) {
        if (items.isEmpty()) return;
        int first = list.size();
        list.addAll(items);
        if (playNow || index < 0) play(first); else changed();
        save();
    }

    void replace(List<Item> items, int start) {
        list.clear();
        list.addAll(items);
        play(Math.max(0, Math.min(start, list.size() - 1)));
        save();
    }

    void remove(int i) {
        if (i < 0 || i >= list.size()) return;
        list.remove(i);
        if (i == index) {
            if (list.isEmpty()) { stopPlayback(); index = -1; }
            else play(Math.min(i, list.size() - 1));
        } else if (i < index) index--;
        changed(); save();
    }

    void move(int from, int to) {
        if (from < 0 || from >= list.size() || to < 0 || to >= list.size() || from == to) return;
        Item it = list.remove(from);
        list.add(to, it);
        if (index == from) index = to;
        else if (from < index && to >= index) index--;
        else if (from > index && to <= index) index++;
        changed(); save();
    }

    void clear() { stopPlayback(); list.clear(); index = -1; changed(); save(); }

    void shuffle() {
        if (list.size() < 2) return;
        Item cur = index >= 0 ? list.get(index) : null;
        Collections.shuffle(list, new Random());
        if (cur != null) { list.remove(cur); list.add(0, cur); index = 0; }
        changed(); save();
    }

    // ---- playback -----------------------------------------------------------

    void play(int i) { play(i, true); }

    void play(int i, boolean autoplay) {
        if (i < 0 || i >= list.size()) return;
        index = i;
        Item it = list.get(i);
        prepared = false; wantPlay = autoplay; videoW = videoH = 0; art = null;
        try {
            if (mp == null) {
                mp = new MediaPlayer();
                mp.setWakeMode(getApplicationContext(), PowerManager.PARTIAL_WAKE_LOCK);
                mp.setOnPreparedListener(p -> {
                    prepared = true;
                    applyFx();
                    applySpeed();
                    Item now = index >= 0 && index < list.size() ? list.get(index) : null;
                    int resumeAt = prefs.getInt("resume_pos", 0);
                    if (resumeNext && now != null && resumeAt > 0 && now.uri.equals(prefs.getString("resume_uri", ""))) p.seekTo(resumeAt);
                    resumeNext = false;
                    if (wantPlay && requestFocus()) p.start();
                    changed();
                });
                mp.setOnCompletionListener(p -> onDone());
                mp.setOnErrorListener((p, what, extra) -> {
                    Log.w(TAG, "playback error " + what + "/" + extra);
                    if (listener != null) listener.onPlayerError(index >= 0 && index < list.size() ? list.get(index).title : "");
                    prepared = false;
                    main.postDelayed(() -> { if (index < list.size() - 1) next(false); else changed(); }, 400);
                    return true;
                });
                mp.setOnVideoSizeChangedListener((p, w, h) -> {
                    videoW = w; videoH = h;
                    if (listener != null) listener.onVideoSize(w, h);
                    changed();
                });
            } else {
                mp.reset();
            }
            mp.setAudioAttributes(new AudioAttributes.Builder()
                    .setUsage(AudioAttributes.USAGE_MEDIA)
                    .setContentType("video".equals(it.kind) ? AudioAttributes.CONTENT_TYPE_MOVIE : AudioAttributes.CONTENT_TYPE_MUSIC)
                    .build());
            mp.setDataSource(this, Uri.parse(it.uri));
            mp.setSurface(surface);
            mp.prepareAsync();
        } catch (Exception e) {
            Log.w(TAG, "cannot open " + it.uri, e);
            if (listener != null) listener.onPlayerError(it.title);
            main.postDelayed(() -> { if (index < list.size() - 1) next(false); }, 400);
        }
        loadArtAsync(it);
        updateSession();
        changed();
        save();
    }

    void onDone() {
        if ("one".equals(repeat)) { seek(0); mp.start(); return; }
        if (index < list.size() - 1) next(false);
        else if ("all".equals(repeat) && !list.isEmpty()) play(0);
        else { wantPlay = false; updateSession(); changed(); }
    }

    void next(boolean user) {
        if (list.isEmpty()) return;
        if (index < list.size() - 1) play(index + 1);
        else if (user || "all".equals(repeat)) play(0);
    }

    void prev() {
        if (list.isEmpty()) return;
        if (position() > 3000) { seek(0); return; }
        play(index > 0 ? index - 1 : 0);
    }

    void toggle() { if (isPlaying()) pause(); else resume(); }

    void resume() {
        if (mp == null || !prepared) { if (index >= 0) play(index); return; }
        wantPlay = true;
        if (requestFocus()) mp.start();
        updateSession(); changed();
    }

    void pause() {
        wantPlay = false;
        if (mp != null && prepared && mp.isPlaying()) mp.pause();
        updateSession(); changed(); save();
    }

    void stopPlayback() {
        wantPlay = false; prepared = false;
        if (mp != null) mp.reset();
        abandonFocus();
        updateSession();
        stopFg(true);
    }

    void seek(int ms) { if (mp != null && prepared) { mp.seekTo(Math.max(0, ms)); changed(); } }

    boolean isPlaying() { try { return mp != null && prepared && mp.isPlaying(); } catch (Exception e) { return false; } }
    int position() { try { return mp != null && prepared ? mp.getCurrentPosition() : 0; } catch (Exception e) { return 0; } }
    int duration() { try { return mp != null && prepared ? mp.getDuration() : 0; } catch (Exception e) { return 0; } }

    void setSurface(Surface s) {
        surface = s;
        if (mp != null) { try { mp.setSurface(s); } catch (Exception ignored) {} }
    }

    void setSpeed(float s) { speed = Math.max(0.25f, Math.min(4f, s)); applySpeed(); changed(); }

    void applySpeed() {
        if (mp == null || !prepared || Build.VERSION.SDK_INT < 23) return;
        try {
            boolean was = mp.isPlaying();
            PlaybackParams pp = mp.getPlaybackParams();
            pp.setSpeed(speed);
            mp.setPlaybackParams(pp); // this starts playback: restore the paused state
            if (!was) mp.pause();
        } catch (Exception e) { Log.w(TAG, "speed", e); }
    }

    // Audio tracks inside a file (films with several languages)
    JSONArray audioTracks() {
        JSONArray a = new JSONArray();
        if (mp == null || !prepared) return a;
        try {
            MediaPlayer.TrackInfo[] ti = mp.getTrackInfo();
            int sel = Build.VERSION.SDK_INT >= 21 ? mp.getSelectedTrack(MediaPlayer.TrackInfo.MEDIA_TRACK_TYPE_AUDIO) : -1;
            for (int i = 0; i < ti.length; i++) {
                if (ti[i].getTrackType() != MediaPlayer.TrackInfo.MEDIA_TRACK_TYPE_AUDIO) continue;
                a.put(new JSONObject().put("id", i).put("lang", ti[i].getLanguage()).put("selected", i == sel));
            }
        } catch (Exception ignored) {}
        return a;
    }

    void selectTrack(int id) { try { if (mp != null && prepared) mp.selectTrack(id); } catch (Exception e) { Log.w(TAG, "track", e); } }

    // ---- audio focus, headphones -------------------------------------------

    boolean resumeAfterFocus;

    final AudioManager.OnAudioFocusChangeListener focus = change -> {
        if (change == AudioManager.AUDIOFOCUS_LOSS) pause();
        else if (change == AudioManager.AUDIOFOCUS_LOSS_TRANSIENT) { boolean w = isPlaying(); pause(); wantPlay = w; resumeAfterFocus = w; }
        else if (change == AudioManager.AUDIOFOCUS_LOSS_TRANSIENT_CAN_DUCK) { if (mp != null) mp.setVolume(0.3f, 0.3f); }
        else if (change == AudioManager.AUDIOFOCUS_GAIN) {
            if (mp != null) mp.setVolume(1f, 1f);
            if (resumeAfterFocus) { resumeAfterFocus = false; resume(); }
        }
    };

    boolean requestFocus() {
        int r;
        if (Build.VERSION.SDK_INT >= 26) {
            if (focusReq == null) focusReq = new AudioFocusRequest.Builder(AudioManager.AUDIOFOCUS_GAIN)
                    .setAudioAttributes(new AudioAttributes.Builder().setUsage(AudioAttributes.USAGE_MEDIA).build())
                    .setOnAudioFocusChangeListener(focus).build();
            r = am.requestAudioFocus(focusReq);
        } else {
            r = am.requestAudioFocus(focus, AudioManager.STREAM_MUSIC, AudioManager.AUDIOFOCUS_GAIN);
        }
        if (!noisyRegistered) {
            registerReceiver(noisy, new IntentFilter(AudioManager.ACTION_AUDIO_BECOMING_NOISY));
            noisyRegistered = true;
        }
        startFg();
        return r == AudioManager.AUDIOFOCUS_REQUEST_GRANTED;
    }

    void abandonFocus() {
        if (Build.VERSION.SDK_INT >= 26 && focusReq != null) am.abandonAudioFocusRequest(focusReq);
        else am.abandonAudioFocus(focus);
    }

    final BroadcastReceiver noisy = new BroadcastReceiver() {
        @Override public void onReceive(Context c, Intent i) { pause(); } // headphones unplugged
    };

    // ---- audio effects --------------------------------------------------------

    void setFx(boolean eqOn, double pre, double[] b, boolean norm, int amp) {
        this.eqOn = eqOn; this.preamp = pre; this.normOn = norm; this.amp = Math.max(100, Math.min(500, amp));
        System.arraycopy(b, 0, bands, 0, Math.min(10, b.length));
        applyFx();
        prefs.edit().putString("fx", fxJson().toString()).apply();
    }

    JSONObject fxJson() {
        try {
            JSONArray b = new JSONArray();
            for (double v : bands) b.put(v);
            return new JSONObject().put("eq", eqOn).put("pre", preamp).put("bands", b).put("norm", normOn).put("amp", amp)
                    .put("deviceBands", eq != null ? eq.getNumberOfBands() : 0);
        } catch (Exception e) { return new JSONObject(); }
    }

    void applyFx() {
        if (mp == null) return;
        int sid = mp.getAudioSessionId();
        if (sid != fxSession) { releaseFx(); fxSession = sid; }
        try {
            if (eq == null) eq = new Equalizer(0, sid);
            short[] range = eq.getBandLevelRange();
            short n = eq.getNumberOfBands();
            for (short b = 0; b < n; b++) {
                double f = eq.getCenterFreq(b) / 1000.0;
                double g = eqOn ? curveAt(f) + preamp : 0;
                int mb = (int) Math.round(g * 100);
                eq.setBandLevel(b, (short) Math.max(range[0], Math.min(range[1], mb)));
            }
            eq.setEnabled(eqOn);
        } catch (Throwable t) { Log.w(TAG, "equalizer", t); }
        try {
            int mb = (int) Math.round(2000 * Math.log10(amp / 100.0)); // 500 % = +14 dB
            if (le == null) le = new LoudnessEnhancer(sid);
            le.setTargetGain(mb);
            le.setEnabled(mb > 0);
        } catch (Throwable t) { Log.w(TAG, "amplifier", t); }
        if (Build.VERSION.SDK_INT >= 28) {
            try {
                if (normOn && dyn == null) {
                    DynamicsProcessing.Config cfg = new DynamicsProcessing.Config.Builder(
                            DynamicsProcessing.VARIANT_FAVOR_FREQUENCY_RESOLUTION, 2, false, 0, true, 1, false, 0, true).build();
                    dyn = new DynamicsProcessing(0, sid, cfg);
                    for (int ch = 0; ch < 2; ch++) {
                        DynamicsProcessing.MbcBand mbc = new DynamicsProcessing.MbcBand(true, 20000f, 3f, 80f, 3.5f, -30f, 6f, -90f, 1f, 0f, 6f);
                        dyn.setMbcBandByChannelIndex(ch, 0, mbc);
                        dyn.setLimiterByChannelIndex(ch, new DynamicsProcessing.Limiter(true, true, 0, 1f, 60f, 10f, -2f, 0f));
                    }
                }
                if (dyn != null) dyn.setEnabled(normOn);
            } catch (Throwable t) { Log.w(TAG, "normalize", t); }
        }
    }

    /** our 10-band curve at frequency f (Hz), interpolated on a log scale */
    double curveAt(double f) {
        if (f <= FREQS[0]) return bands[0];
        for (int i = 1; i < FREQS.length; i++) {
            if (f <= FREQS[i]) {
                double t = (Math.log(f) - Math.log(FREQS[i - 1])) / (Math.log(FREQS[i]) - Math.log(FREQS[i - 1]));
                return bands[i - 1] + t * (bands[i] - bands[i - 1]);
            }
        }
        return bands[9];
    }

    void releaseFx() {
        try { if (eq != null) eq.release(); } catch (Throwable ignored) {}
        try { if (le != null) le.release(); } catch (Throwable ignored) {}
        try { if (dyn != null) dyn.release(); } catch (Throwable ignored) {}
        eq = null; le = null; dyn = null;
    }

    // ---- media session and notification -------------------------------------

    void loadArtAsync(Item it) {
        Thumbs.get(this, it.uri, it.kind, 512, bmp -> main.post(() -> {
            if (index >= 0 && index < list.size() && list.get(index) == it) { art = bmp; updateSession(); }
        }));
    }

    void updateSession() {
        Item it = index >= 0 && index < list.size() ? list.get(index) : null;
        MediaMetadata.Builder md = new MediaMetadata.Builder();
        if (it != null) md.putString(MediaMetadata.METADATA_KEY_TITLE, it.title).putLong(MediaMetadata.METADATA_KEY_DURATION, duration());
        if (art != null) md.putBitmap(MediaMetadata.METADATA_KEY_ALBUM_ART, art);
        session.setMetadata(md.build());
        boolean playing = wantPlay;
        session.setPlaybackState(new PlaybackState.Builder()
                .setActions(PlaybackState.ACTION_PLAY | PlaybackState.ACTION_PAUSE | PlaybackState.ACTION_PLAY_PAUSE
                        | PlaybackState.ACTION_SKIP_TO_NEXT | PlaybackState.ACTION_SKIP_TO_PREVIOUS | PlaybackState.ACTION_SEEK_TO | PlaybackState.ACTION_STOP)
                .setState(playing ? PlaybackState.STATE_PLAYING : PlaybackState.STATE_PAUSED, position(), playing ? speed : 0f)
                .build());
        if (foreground || playing) startFg();
    }

    PendingIntent action(String a, int code) {
        Intent i = new Intent(this, PlayerService.class).setAction(a);
        return PendingIntent.getService(this, code, i, PendingIntent.FLAG_UPDATE_CURRENT | PendingIntent.FLAG_IMMUTABLE);
    }

    Notification buildNote() {
        Item it = index >= 0 && index < list.size() ? list.get(index) : null;
        Notification.Builder b = Build.VERSION.SDK_INT >= 26 ? new Notification.Builder(this, CH) : new Notification.Builder(this);
        Intent open = new Intent(this, MainActivity.class).addFlags(Intent.FLAG_ACTIVITY_SINGLE_TOP);
        b.setSmallIcon(R.drawable.ic_notify)
                .setContentTitle(it != null ? it.title : "OpenPlayer")
                .setContentText("OpenPlayer")
                .setLargeIcon(art)
                .setContentIntent(PendingIntent.getActivity(this, 1, open, PendingIntent.FLAG_UPDATE_CURRENT | PendingIntent.FLAG_IMMUTABLE))
                .setDeleteIntent(action(ACT_STOP, 5))
                .setVisibility(Notification.VISIBILITY_PUBLIC)
                .setOngoing(wantPlay)
                .setShowWhen(false)
                .addAction(new Notification.Action.Builder(android.R.drawable.ic_media_previous, "Previous", action(ACT_PREV, 2)).build())
                .addAction(new Notification.Action.Builder(wantPlay ? android.R.drawable.ic_media_pause : android.R.drawable.ic_media_play,
                        wantPlay ? "Pause" : "Play", action(ACT_TOGGLE, 3)).build())
                .addAction(new Notification.Action.Builder(android.R.drawable.ic_media_next, "Next", action(ACT_NEXT, 4)).build())
                .setStyle(new Notification.MediaStyle().setMediaSession(session.getSessionToken()).setShowActionsInCompactView(0, 1, 2));
        return b.build();
    }

    void startFg() {
        if (index < 0) return;
        try {
            Notification n = buildNote();
            if (!foreground) {
                if (Build.VERSION.SDK_INT >= 29) startForeground(NOTE_ID, n, ServiceInfo.FOREGROUND_SERVICE_TYPE_MEDIA_PLAYBACK);
                else startForeground(NOTE_ID, n);
                foreground = true;
            } else {
                ((NotificationManager) getSystemService(NOTIFICATION_SERVICE)).notify(NOTE_ID, n);
            }
            if (!wantPlay && Build.VERSION.SDK_INT >= 24) { stopForeground(STOP_FOREGROUND_DETACH); foreground = false; }
        } catch (Exception e) { Log.w(TAG, "notification", e); }
    }

    void stopFg(boolean remove) {
        if (Build.VERSION.SDK_INT >= 24) stopForeground(remove ? STOP_FOREGROUND_REMOVE : STOP_FOREGROUND_DETACH);
        else stopForeground(remove);
        foreground = false;
    }

    // ---- state for the screen, saving -------------------------------------------

    final Runnable tick = new Runnable() {
        @Override public void run() {
            if (sleepAt > 0 && System.currentTimeMillis() >= sleepAt) { sleepAt = 0; pause(); }
            if (isPlaying()) changed();
            main.postDelayed(this, 1000);
        }
    };

    void changed() { if (listener != null) listener.onPlayerChanged(); }

    JSONObject state() {
        JSONObject o = new JSONObject();
        try {
            JSONArray a = new JSONArray();
            for (Item it : list) a.put(it.json());
            Item cur = index >= 0 && index < list.size() ? list.get(index) : null;
            o.put("list", a).put("index", index).put("playing", isPlaying() || (wantPlay && !prepared && cur != null))
                    .put("pos", position()).put("dur", duration()).put("title", cur != null ? cur.title : "")
                    .put("kind", cur != null ? cur.kind : "").put("uri", cur != null ? cur.uri : "")
                    .put("video", videoW > 0 && videoH > 0).put("w", videoW).put("h", videoH)
                    .put("speed", speed).put("repeat", repeat).put("sleep", sleepAt > 0 ? (sleepAt - System.currentTimeMillis()) / 1000 : 0)
                    .put("tracks", audioTracks());
        } catch (Exception ignored) {}
        return o;
    }

    void save() {
        try {
            JSONArray a = new JSONArray();
            for (Item it : list) a.put(it.json());
            SharedPreferences.Editor e = prefs.edit().putString("list", a.toString()).putInt("index", index).putString("repeat", repeat);
            Item cur = index >= 0 && index < list.size() ? list.get(index) : null;
            if (cur != null && position() > 0) e.putString("resume_uri", cur.uri).putInt("resume_pos", position());
            e.apply();
        } catch (Exception ignored) {}
    }

    void restore() {
        try {
            JSONArray a = new JSONArray(prefs.getString("list", "[]"));
            for (int i = 0; i < a.length(); i++) {
                JSONObject o = a.getJSONObject(i);
                list.add(new Item(o.getString("uri"), o.optString("title"), o.optString("kind", "audio")));
            }
            index = Math.min(prefs.getInt("index", -1), list.size() - 1);
            repeat = prefs.getString("repeat", "off");
            JSONObject fx = new JSONObject(prefs.getString("fx", "{}"));
            eqOn = fx.optBoolean("eq"); preamp = fx.optDouble("pre", 0); normOn = fx.optBoolean("norm"); amp = fx.optInt("amp", 100);
            JSONArray b = fx.optJSONArray("bands");
            if (b != null) for (int i = 0; i < Math.min(10, b.length()); i++) bands[i] = b.optDouble(i, 0);
        } catch (Exception e) { Log.w(TAG, "restore", e); }
    }

    /** prepares the remembered song without starting it, so Play continues where it stopped */
    void prepareRemembered(boolean resumePos) {
        if (index >= 0 && index < list.size() && mp == null) {
            resumeNext = resumePos;
            play(index, false);
        }
    }
}
