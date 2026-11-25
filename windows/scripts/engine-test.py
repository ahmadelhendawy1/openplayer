#!/usr/bin/env python3
"""Engine test: a candidate libmpv must pass this before the robot publishes it.

    python engine-test.py <libmpv-2.dll | libmpv.so.2> <testmedia-dir> [<uosc-dir>]

For every file in testmedia-dir it checks that the engine opens it, plays at
least one second, decodes the audio track AND produces real (non-silent)
sound through OpenPlayer's heaviest audio chain (normalize + resample + the
10-band equalizer + amplifier + limiter). This is exactly the chain that once
made 32 kHz phone videos silent. If a uosc directory is given, the on-screen
controls script must load without errors.

Exit code 0 = engine accepted, 1 = rejected. Prints one line per check.
"""
import ctypes
import math
import os
import struct
import sys
import tempfile
import time
import wave

# OpenPlayer's heaviest audio chain (Normalize on, 10 bands incl. 16 kHz, amp 200 %).
AF = ("@op:lavfi=[dynaudnorm@norm=f=150:g=15:p=0.95:m=10,aresample@sr=48000,volume@pre=volume=6dB,"
      "equalizer@b0=f=31:t=o:w=1:g=-3,equalizer@b1=f=62:t=o:w=1:g=-1,equalizer@b2=f=125:t=o:w=1:g=1,"
      "equalizer@b3=f=250:t=o:w=1:g=2,equalizer@b4=f=500:t=o:w=1:g=3,equalizer@b5=f=1000:t=o:w=1:g=2,"
      "equalizer@b6=f=2000:t=o:w=1:g=2,equalizer@b7=f=4000:t=o:w=1:g=2,equalizer@b8=f=8000:t=o:w=1:g=3,"
      "equalizer@b9=f=16000:t=o:w=1:g=8.5,volume@amp=volume=2,"
      "alimiter@lim=limit=0.98:attack=5:release=50:level=disabled]")

MEDIA_EXT = {".mp4", ".mkv", ".webm", ".mp3", ".flac", ".m4a", ".ogg", ".avi", ".mov", ".ts", ".wav"}
EV_SHUTDOWN, EV_LOG, EV_END_FILE, EV_FILE_LOADED = 1, 2, 7, 8


class Event(ctypes.Structure):
    _fields_ = [("event_id", ctypes.c_int), ("error", ctypes.c_int),
                ("reply_userdata", ctypes.c_uint64), ("data", ctypes.c_void_p)]


class LogMessage(ctypes.Structure):
    _fields_ = [("prefix", ctypes.c_char_p), ("level", ctypes.c_char_p), ("text", ctypes.c_char_p),
                ("log_level", ctypes.c_int)]


class EndFile(ctypes.Structure):
    _fields_ = [("reason", ctypes.c_int), ("error", ctypes.c_int)]


class Engine:
    def __init__(self, path):
        if os.name == "nt":
            os.add_dll_directory(os.path.dirname(os.path.abspath(path)))
        self.lib = lib = ctypes.CDLL(os.path.abspath(path))
        lib.mpv_create.restype = ctypes.c_void_p
        lib.mpv_client_api_version.restype = ctypes.c_ulong
        lib.mpv_wait_event.restype = ctypes.POINTER(Event)
        lib.mpv_wait_event.argtypes = [ctypes.c_void_p, ctypes.c_double]
        lib.mpv_get_property_string.restype = ctypes.c_void_p
        lib.mpv_get_property_string.argtypes = [ctypes.c_void_p, ctypes.c_char_p]
        lib.mpv_free.argtypes = [ctypes.c_void_p]
        lib.mpv_set_option_string.argtypes = [ctypes.c_void_p, ctypes.c_char_p, ctypes.c_char_p]
        lib.mpv_command.argtypes = [ctypes.c_void_p, ctypes.POINTER(ctypes.c_char_p)]
        lib.mpv_request_log_messages.argtypes = [ctypes.c_void_p, ctypes.c_char_p]
        lib.mpv_initialize.argtypes = [ctypes.c_void_p]
        lib.mpv_terminate_destroy.argtypes = [ctypes.c_void_p]
        self.api = lib.mpv_client_api_version()
        self.h = lib.mpv_create()
        if not self.h:
            raise RuntimeError("mpv_create failed")
        self.errors = []

    def opt(self, k, v):
        r = self.lib.mpv_set_option_string(self.h, k.encode(), v.encode())
        if r < 0:
            raise RuntimeError("option %s=%s rejected (%d)" % (k, v, r))

    def start(self):
        if self.lib.mpv_initialize(self.h) < 0:
            raise RuntimeError("mpv_initialize failed")
        self.lib.mpv_request_log_messages(self.h, b"warn")

    def cmd(self, *args):
        arr = (ctypes.c_char_p * (len(args) + 1))(*[a.encode() for a in args], None)
        return self.lib.mpv_command(self.h, arr)

    def get(self, name):
        p = self.lib.mpv_get_property_string(self.h, name.encode())
        if not p:
            return None
        s = ctypes.string_at(p).decode("utf-8", "replace")
        self.lib.mpv_free(p)
        return s

    def events(self, timeout):
        """yield events until timeout (seconds) passes"""
        end = time.time() + timeout
        while time.time() < end:
            e = self.lib.mpv_wait_event(self.h, 0.05).contents
            if e.event_id == 0:
                continue
            if e.event_id == EV_LOG and e.data:
                m = ctypes.cast(e.data, ctypes.POINTER(LogMessage)).contents
                prefix = (m.prefix or b"").decode("utf-8", "replace")
                level = (m.level or b"").decode()
                text = (m.text or b"").decode("utf-8", "replace").strip()
                if level in ("fatal", "error"):
                    self.errors.append("%s: %s" % (prefix, text))
            yield e

    def close(self):
        self.lib.mpv_terminate_destroy(self.h)


def rms_db(path):
    """(level in dB, seconds of audio written)"""
    with wave.open(path, "rb") as w:
        n, width, ch, rate = w.getnframes(), w.getsampwidth(), w.getnchannels(), w.getframerate()
        data = w.readframes(n)
    secs = len(data) / float(width * ch * rate) if rate and width and ch else 0.0
    if width != 2 or not data:
        return -200.0, secs
    samples = struct.unpack("<%dh" % (len(data) // 2), data[: len(data) // 2 * 2])
    if not samples:
        return -200.0, secs
    mean = sum(s * s for s in samples) / len(samples)
    return (10 * math.log10(mean / (32768.0 ** 2)) if mean > 0 else -200.0), secs


def play(lib_path, media, wav, uosc=None):
    """returns (ok, details)"""
    e = Engine(lib_path)
    try:
        for k, v in (("vo", "null"), ("ao", "pcm"), ("ao-pcm-file", wav), ("ao-pcm-waveheader", "yes"),
                     ("audio-format", "s16"), ("af", AF), ("keep-open", "no"), ("idle", "yes"),
                     ("hwdec", "no"), ("terminal", "no"), ("config", "no")):
            e.opt(k, v)
        e.start()
        if uosc:
            e.cmd("load-script", uosc)
        e.cmd("loadfile", media)
        loaded, played, info = False, 0.0, {}
        for ev in e.events(15):
            if ev.event_id == EV_FILE_LOADED:
                loaded = True
                info = {"audio": e.get("audio-codec-name"), "video": e.get("video-codec"),
                        "rate": e.get("audio-params/samplerate")}
            elif ev.event_id == EV_END_FILE:
                break
            if loaded:
                try:
                    played = max(played, float(e.get("playback-time") or 0))
                except ValueError:
                    pass
        e.cmd("quit")
        for _ in e.events(2):
            pass
        errors = [x for x in e.errors if not x.startswith("ffmpeg/demuxer")]
        return loaded, played, info, errors
    finally:
        e.close()


def main():
    if len(sys.argv) < 3:
        print(__doc__)
        return 1
    lib_path, media_dir = sys.argv[1], sys.argv[2]
    uosc = sys.argv[3] if len(sys.argv) > 3 else None
    failed = 0
    api = Engine(lib_path)
    major = api.api >> 16
    api.close()
    print("engine API version %d.%d" % (major, api.api & 0xFFFF))
    if major != 2:
        print("FAIL  OpenPlayer needs libmpv client API 2.x")
        return 1
    files = sorted(f for f in os.listdir(media_dir) if os.path.splitext(f)[1].lower() in MEDIA_EXT)
    if not files:
        print("FAIL  no test media in", media_dir)
        return 1
    tmp = tempfile.mkdtemp()
    for i, f in enumerate(files):
        wav = os.path.join(tmp, "out%d.wav" % i)
        loaded, played, info, errors = play(lib_path, os.path.join(media_dir, f), wav, uosc if i == 0 else None)
        # the null outputs run faster than real time, so the length of the sound
        # written (not the clock) shows how much was really played
        level, secs = rms_db(wav) if os.path.exists(wav) else (-200.0, 0.0)
        problems = []
        if not loaded:
            problems.append("did not open")
        if secs < 1.5:
            problems.append("played only %.1f s of sound" % secs)
        if not info.get("audio"):
            problems.append("no audio decoded")
        if level < -45:
            problems.append("SILENT output (%.1f dB) through OpenPlayer's audio chain" % level)
        if level > -2.0:  # the test tones come out near -4 dB; a broken filter gives full-scale noise
            problems.append("BROKEN output (%.1f dB, full-scale noise) through OpenPlayer's audio chain" % level)
        if errors:
            problems.append("errors: " + " | ".join(errors[:3]))
        status = "ok  " if not problems else "FAIL"
        print("%s  %-24s audio=%s video=%s played=%.1fs level=%.1f dB%s" % (
            status, f, info.get("audio"), (info.get("video") or "-").split(" ")[0], secs, level,
            ("  -> " + "; ".join(problems)) if problems else ""))
        failed += bool(problems)
    if uosc:
        print("ok    controls script loaded with %s" % ("errors (see above)" if failed else "no errors"))
    print("RESULT: %s (%d of %d files passed)" % ("ACCEPTED" if not failed else "REJECTED", len(files) - failed, len(files)))
    return 1 if failed else 0


if __name__ == "__main__":
    sys.exit(main())
