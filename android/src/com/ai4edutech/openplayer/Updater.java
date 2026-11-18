package com.ai4edutech.openplayer;

import android.app.PendingIntent;
import android.content.Context;
import android.content.Intent;
import android.content.pm.PackageInstaller;
import android.os.Build;
import android.util.Log;

import org.json.JSONArray;
import org.json.JSONObject;

import java.io.File;
import java.io.FileInputStream;
import java.io.FileOutputStream;
import java.io.InputStream;
import java.io.OutputStream;
import java.net.HttpURLConnection;
import java.net.URL;
import java.security.MessageDigest;

/**
 * "New version" check and self-update, same idea as the Windows player:
 * latest.json on the download site lists the Android release under "android".
 * The APK is downloaded, its SHA-256 is checked against latest.json, then
 * Android's own installer is asked to update the app (the user confirms).
 */
final class Updater {
    static final String LATEST = "https://ai4edutech.com/apps/openplayer/latest.json";
    static final String SITE = "https://ai4edutech.com/apps/openplayer/";
    static final String ACTION_INSTALLED = "com.ai4edutech.openplayer.INSTALL_STATUS";

    interface Progress { void at(int pct, long got, long total); }

    static class Info {
        String version = "", file = "", sha256 = "", url = SITE, date = "", notesEn = "", notesAr = "";
        int code; double sizeMb; boolean force;
        JSONObject json(String current, int currentCode, String lang) throws Exception {
            String notes = "ar".equals(lang) && !notesAr.isEmpty() ? notesAr : notesEn;
            JSONArray n = new JSONArray();
            for (String line : notes.split("\n")) { line = line.replaceFirst("^\\s*[-•*]\\s*", "").trim(); if (!line.isEmpty()) n.put(line); }
            return new JSONObject().put("version", version).put("current", current).put("newer", isNewer(this, current, currentCode))
                    .put("date", date).put("size", sizeMb > 0 ? String.format(java.util.Locale.US, "%.1f", sizeMb) : "")
                    .put("notes", n).put("canSelf", !file.isEmpty() && sha256.length() == 64).put("force", force);
        }
    }

    private Updater() {}

    static Info fetch() throws Exception {
        HttpURLConnection c = (HttpURLConnection) new URL(LATEST + "?t=" + System.currentTimeMillis() / 60000).openConnection();
        c.setConnectTimeout(15000); c.setReadTimeout(15000);
        c.setRequestProperty("User-Agent", "OpenPlayer-Android/" + BuildInfo.VERSION);
        c.setRequestProperty("Cache-Control", "no-cache");
        if (c.getResponseCode() != 200) throw new Exception("HTTP " + c.getResponseCode());
        byte[] b = readAll(c.getInputStream(), 64 << 10);
        JSONObject o = new JSONObject(new String(b, "UTF-8"));
        JSONObject a = o.optJSONObject("android");
        if (a == null) throw new Exception("no Android release listed yet");
        Info i = new Info();
        i.version = a.optString("version");
        i.code = a.optInt("version_code", 0);
        i.file = a.optString("file");
        i.sha256 = a.optString("sha256").toLowerCase(java.util.Locale.US);
        i.sizeMb = a.optDouble("size_mb", 0);
        i.date = a.optString("date", o.optString("date"));
        i.notesEn = a.optString("notes_en", o.optString("notes_en"));
        i.notesAr = a.optString("notes_ar", o.optString("notes_ar"));
        i.force = a.optBoolean("force", false);
        i.url = o.optString("url", SITE);
        if (i.version.isEmpty()) throw new Exception("invalid latest.json");
        return i;
    }

    static boolean isNewer(Info i, String current, int currentCode) {
        if (i.code > 0) return i.code > currentCode;
        return compare(i.version, current) > 0;
    }

    static int compare(String a, String b) {
        String[] x = a.split("\\."), y = b.split("\\.");
        for (int k = 0; k < Math.max(x.length, y.length); k++) {
            int p = k < x.length ? num(x[k]) : 0, q = k < y.length ? num(y[k]) : 0;
            if (p != q) return p < q ? -1 : 1;
        }
        return 0;
    }

    static int num(String s) { try { return Integer.parseInt(s.replaceAll("[^0-9].*$", "")); } catch (Exception e) { return 0; } }

    /** downloads the APK next to latest.json and checks it; returns the file */
    static File download(Context ctx, Info i, Progress p, boolean[] cancel) throws Exception {
        URL base = new URL(LATEST);
        URL u = new URL(base, i.file.replace(" ", "%20"));
        if (!"https".equals(u.getProtocol())) throw new Exception("insecure link");
        File dir = new File(ctx.getCacheDir(), "updates");
        //noinspection ResultOfMethodCallIgnored
        dir.mkdirs();
        File out = new File(dir, "OpenPlayer-" + i.version.replaceAll("[^0-9.]", "") + ".apk");
        File part = new File(dir, out.getName() + ".part");
        HttpURLConnection c = (HttpURLConnection) u.openConnection();
        c.setConnectTimeout(20000); c.setReadTimeout(30000);
        c.setRequestProperty("User-Agent", "OpenPlayer-Android/" + BuildInfo.VERSION);
        if (c.getResponseCode() != 200) throw new Exception("HTTP " + c.getResponseCode());
        long total = c.getContentLengthLong(), got = 0;
        MessageDigest md = MessageDigest.getInstance("SHA-256");
        try (InputStream in = c.getInputStream(); OutputStream os = new FileOutputStream(part)) {
            byte[] buf = new byte[64 << 10];
            int n; long last = 0;
            while ((n = in.read(buf)) > 0) {
                if (cancel[0]) throw new InterruptedException("cancelled");
                os.write(buf, 0, n); md.update(buf, 0, n); got += n;
                if (System.currentTimeMillis() - last > 200) { last = System.currentTimeMillis(); p.at(total > 0 ? (int) (got * 100 / total) : 0, got, total); }
            }
        }
        if (!hex(md.digest()).equals(i.sha256)) {
            //noinspection ResultOfMethodCallIgnored
            part.delete();
            throw new SecurityException("hash");
        }
        //noinspection ResultOfMethodCallIgnored
        out.delete();
        if (!part.renameTo(out)) throw new Exception("cannot save the update");
        p.at(100, got, total);
        return out;
    }

    /** hands the checked APK to Android's installer; the result comes back as a broadcast */
    static void install(Context ctx, File apk) throws Exception {
        PackageInstaller pi = ctx.getPackageManager().getPackageInstaller();
        PackageInstaller.SessionParams sp = new PackageInstaller.SessionParams(PackageInstaller.SessionParams.MODE_FULL_INSTALL);
        sp.setAppPackageName(ctx.getPackageName());
        sp.setSize(apk.length());
        if (Build.VERSION.SDK_INT >= 31) sp.setRequireUserAction(PackageInstaller.SessionParams.USER_ACTION_NOT_REQUIRED);
        int id = pi.createSession(sp);
        try (PackageInstaller.Session s = pi.openSession(id)) {
            try (InputStream in = new FileInputStream(apk); OutputStream os = s.openWrite("base.apk", 0, apk.length())) {
                byte[] buf = new byte[64 << 10];
                int n;
                while ((n = in.read(buf)) > 0) os.write(buf, 0, n);
                s.fsync(os);
            }
            Intent i = new Intent(ACTION_INSTALLED).setPackage(ctx.getPackageName());
            int flags = PendingIntent.FLAG_UPDATE_CURRENT | (Build.VERSION.SDK_INT >= 31 ? PendingIntent.FLAG_MUTABLE : 0);
            s.commit(PendingIntent.getBroadcast(ctx, id, i, flags).getIntentSender());
        }
    }

    static byte[] readAll(InputStream in, int limit) throws Exception {
        try (InputStream s = in) {
            java.io.ByteArrayOutputStream bo = new java.io.ByteArrayOutputStream();
            byte[] buf = new byte[8192];
            int n;
            while ((n = s.read(buf)) > 0) { bo.write(buf, 0, n); if (bo.size() > limit) throw new Exception("too large"); }
            return bo.toByteArray();
        }
    }

    static String hex(byte[] b) {
        StringBuilder sb = new StringBuilder();
        for (byte x : b) sb.append(String.format("%02x", x));
        return sb.toString();
    }

    static void log(String m, Throwable t) { Log.w(PlayerService.TAG, "update: " + m, t); }
}
