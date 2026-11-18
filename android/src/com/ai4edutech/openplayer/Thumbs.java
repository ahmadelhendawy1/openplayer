package com.ai4edutech.openplayer;

import android.content.Context;
import android.graphics.Bitmap;
import android.graphics.BitmapFactory;
import android.media.MediaMetadataRetriever;
import android.net.Uri;
import android.os.Build;
import android.util.Base64;
import android.util.Size;

import java.io.ByteArrayOutputStream;
import java.io.File;
import java.io.FileOutputStream;
import java.nio.file.Files;
import java.security.MessageDigest;
import java.util.concurrent.ExecutorService;
import java.util.concurrent.Executors;

/**
 * Cover art for songs and a frame for videos, cached as small JPEG files so
 * the playlist and library scroll smoothly.
 */
final class Thumbs {
    interface Done { void got(Bitmap b); }
    interface DoneData { void got(String dataUrl); }

    static final ExecutorService pool = Executors.newFixedThreadPool(2);

    private Thumbs() {}

    static File cacheFile(Context c, String uri, int size) {
        File dir = new File(c.getCacheDir(), "thumbs");
        //noinspection ResultOfMethodCallIgnored
        dir.mkdirs();
        return new File(dir, sha1(uri) + "_" + size + ".jpg");
    }

    static void get(Context c, String uri, String kind, int size, Done d) {
        pool.execute(() -> d.got(load(c, uri, kind, size)));
    }

    /** a data: URL for the web page, or "" when the file has no picture */
    static void getData(Context c, String uri, String kind, int size, DoneData d) {
        pool.execute(() -> {
            try {
                File f = cacheFile(c, uri, size);
                if (!f.exists()) load(c, uri, kind, size);
                if (f.exists() && f.length() > 0) {
                    d.got("data:image/jpeg;base64," + Base64.encodeToString(Files.readAllBytes(f.toPath()), Base64.NO_WRAP));
                    return;
                }
            } catch (Throwable ignored) {}
            d.got("");
        });
    }

    static Bitmap load(Context c, String uri, String kind, int size) {
        File f = cacheFile(c, uri, size);
        if (f.exists()) {
            if (f.length() == 0) return null; // known: no picture
            Bitmap b = BitmapFactory.decodeFile(f.getPath());
            if (b != null) return b;
        }
        Bitmap b = null;
        Uri u = Uri.parse(uri);
        if (Build.VERSION.SDK_INT >= 29 && "content".equals(u.getScheme())) {
            try { b = c.getContentResolver().loadThumbnail(u, new Size(size, size), null); } catch (Throwable ignored) {}
        }
        if (b == null) {
            MediaMetadataRetriever r = new MediaMetadataRetriever();
            try {
                r.setDataSource(c, u);
                byte[] pic = r.getEmbeddedPicture();
                if (pic != null) {
                    BitmapFactory.Options o = new BitmapFactory.Options();
                    o.inJustDecodeBounds = true;
                    BitmapFactory.decodeByteArray(pic, 0, pic.length, o);
                    o.inSampleSize = Math.max(1, Math.min(o.outWidth, o.outHeight) / size);
                    o.inJustDecodeBounds = false;
                    b = BitmapFactory.decodeByteArray(pic, 0, pic.length, o);
                } else if ("video".equals(kind)) {
                    b = r.getFrameAtTime(5_000_000, MediaMetadataRetriever.OPTION_CLOSEST_SYNC);
                    if (b == null) b = r.getFrameAtTime();
                }
            } catch (Throwable ignored) {
            } finally {
                try { r.release(); } catch (Throwable ignored) {}
            }
        }
        if (b != null) {
            float s = (float) size / Math.max(b.getWidth(), b.getHeight());
            if (s < 1f) b = Bitmap.createScaledBitmap(b, Math.max(1, Math.round(b.getWidth() * s)), Math.max(1, Math.round(b.getHeight() * s)), true);
        }
        try (FileOutputStream out = new FileOutputStream(f)) {
            if (b != null) {
                ByteArrayOutputStream bo = new ByteArrayOutputStream();
                b.compress(Bitmap.CompressFormat.JPEG, 82, bo);
                out.write(bo.toByteArray());
            } // an empty file remembers "no picture"
        } catch (Throwable ignored) {}
        return b;
    }

    static String sha1(String s) {
        try {
            byte[] h = MessageDigest.getInstance("SHA-1").digest(s.getBytes("UTF-8"));
            StringBuilder sb = new StringBuilder();
            for (byte x : h) sb.append(String.format("%02x", x));
            return sb.toString();
        } catch (Exception e) { return Integer.toHexString(s.hashCode()); }
    }
}
