package main

// What's new in THIS version, shown by Help > What's New (and once after an
// update). Keep it the same as notes_en / notes_ar in latest.json.
// Arabic is written in Egyptian colloquial, like the website.
const (
	whatsNewDate = "2026-11-25"
	whatsNewEN   = `Automatic codec updates: OpenPlayer now keeps its playback engine and codecs up to date by itself, with tested and signed updates
Found a problem? Help › Report a Bug sends it straight to the developer (no account needed)
OpenPlayer remembers your volume: turn it down once and every next video (and the next time you open the app) plays at the same volume
Fixed: some videos (with 32 kHz or 16 kHz sound, like many phone and WhatsApp videos) played with no voice when the equalizer was on — including videos you had opened before
Clicking the video now pauses and plays instantly
OpenPlayer remembers where you stopped: open it again and your last video is waiting at the same moment
When you continue a video it goes back 5 seconds so you remember what was happening (change it in Tools › Options)
The timeline shows time played on the left, and time left / total length on the right
When a video ends, press Play to watch it again from the beginning
A video you watched to the end opens from the beginning next time
Click the video once to pause, click again to play
Double-click the video to go full screen, double-click again to leave full screen
In full screen the controls hide by themselves after 3 seconds when paused
Help menu: What's New, Check for Updates, website and email support`
	whatsNewAR = `تحديث الكوديك أوتوماتيك: البرنامج بقى بيحدّث محرك التشغيل والكوديكات لوحده، بتحديثات متجرّبة وموقّعة
لقيت مشكلة؟ من المساعدة › الإبلاغ عن مشكلة، البلاغ يوصل للمطوّر على طول (من غير ما تعمل حساب)
بقى OpenPlayer بيفتكر مستوى الصوت: وطّيه مرة وكل الفيديوهات اللي بعده (والمرة الجاية اللي تفتح فيها البرنامج) هتشتغل على نفس الصوت
اتصلّحت مشكلة إن بعض الفيديوهات (زي فيديوهات الموبايل والواتساب) كانت بتشتغل من غير صوت لما الإكولايزر يكون شغّال، حتى الفيديوهات اللي اتفتحت قبل كده
الدوسة على الفيديو بقت بتوقف وتشغّل فورًا
بقى OpenPlayer بيفتكر انت وقفت فين: افتحه تاني وهتلاقي آخر فيديو مستنيك عند نفس اللحظة
لما تكمّل فيديو بيرجع 5 ثواني لورا عشان تفتكر كنت فين (تقدر تغيرها من الأدوات › الخيارات)
شريط الوقت بيعرض اللي اتشغّل على الشمال، والباقي / المدة الكاملة على اليمين
لما الفيديو يخلص، دوس تشغيل وهيبدأ من الأول
الفيديو اللي اتفرج عليه للآخر هيفتح من البداية المرة الجاية
دوسة واحدة على الفيديو توقفه، ودوسة كمان تشغّله
دوستين ورا بعض على الفيديو يفتحوه ملء الشاشة، ودوستين كمان يرجّعوه
في ملء الشاشة، أزرار التحكم بتختفي لوحدها بعد 3 ثواني لما توقف الفيديو
قائمة المساعدة: الجديد، البحث عن تحديثات، الموقع والدعم بالإيميل`

	supportEmail = "support@ai4edutech.com"
	websiteURL   = "https://ai4edutech.com/apps/openplayer/"
)
