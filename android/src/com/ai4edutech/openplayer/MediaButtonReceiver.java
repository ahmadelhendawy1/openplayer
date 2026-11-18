package com.ai4edutech.openplayer;

import android.content.BroadcastReceiver;
import android.content.Context;
import android.content.Intent;
import android.view.KeyEvent;

/** Headset and Bluetooth buttons when the media session is not the active one. */
public class MediaButtonReceiver extends BroadcastReceiver {
    @Override public void onReceive(Context c, Intent i) {
        if (!Intent.ACTION_MEDIA_BUTTON.equals(i.getAction())) return;
        KeyEvent k = i.getParcelableExtra(Intent.EXTRA_KEY_EVENT);
        PlayerService s = PlayerService.instance;
        if (k == null || s == null || k.getAction() != KeyEvent.ACTION_DOWN) return;
        switch (k.getKeyCode()) {
            case KeyEvent.KEYCODE_MEDIA_PLAY_PAUSE:
            case KeyEvent.KEYCODE_HEADSETHOOK: s.toggle(); break;
            case KeyEvent.KEYCODE_MEDIA_PLAY: s.resume(); break;
            case KeyEvent.KEYCODE_MEDIA_PAUSE:
            case KeyEvent.KEYCODE_MEDIA_STOP: s.pause(); break;
            case KeyEvent.KEYCODE_MEDIA_NEXT: s.next(true); break;
            case KeyEvent.KEYCODE_MEDIA_PREVIOUS: s.prev(); break;
        }
    }
}
