// OpenPlayer glass plate: real backdrop blur of the video under the floating
// control bar. Runs inside mpv after scaling (OUTPUT hook), so the on-screen
// controls (drawn later as OSD) stay sharp and fully opaque on top.
// Geometry and tint are set at run time through --glsl-shader-opts.

//!PARAM glass_on
//!TYPE float
//!MINIMUM 0.0
//!MAXIMUM 1.0
0.0
//!PARAM glass_x0
//!TYPE float
0.0
//!PARAM glass_y0
//!TYPE float
0.0
//!PARAM glass_x1
//!TYPE float
0.0
//!PARAM glass_y1
//!TYPE float
0.0
//!PARAM glass_r
//!TYPE float
16.0
//!PARAM glass_step
//!TYPE float
2.5
//!PARAM glass_tr
//!TYPE float
0.07
//!PARAM glass_tg
//!TYPE float
0.07
//!PARAM glass_tb
//!TYPE float
0.07
//!PARAM glass_ta
//!TYPE float
0.62
//!PARAM glass_hl
//!TYPE float
0.18

//!HOOK OUTPUT
//!BIND HOOKED
//!SAVE GLASSH
//!DESC OpenPlayer glass: horizontal blur
vec4 hook() {
    vec2 p = HOOKED_pos * HOOKED_size;
    float pad = glass_step * 13.0;
    if (glass_on < 0.5 || p.x < glass_x0 - pad || p.x > glass_x1 + pad || p.y < glass_y0 - pad || p.y > glass_y1 + pad)
        return HOOKED_tex(HOOKED_pos);
    vec4 c = vec4(0.0);
    float w = 0.0;
    for (int i = -12; i <= 12; i++) {
        float k = exp(-float(i * i) / 72.0);
        c += k * HOOKED_tex(HOOKED_pos + vec2(float(i) * glass_step, 0.0) * HOOKED_pt);
        w += k;
    }
    return c / w;
}

//!HOOK OUTPUT
//!BIND HOOKED
//!BIND GLASSH
//!DESC OpenPlayer glass: vertical blur, tint, rounded clip
vec4 hook() {
    vec4 orig = HOOKED_tex(HOOKED_pos);
    if (glass_on < 0.5)
        return orig;
    vec2 p = HOOKED_pos * HOOKED_size;
    vec2 ctr = vec2(glass_x0 + glass_x1, glass_y0 + glass_y1) * 0.5;
    vec2 half_size = vec2(glass_x1 - glass_x0, glass_y1 - glass_y0) * 0.5;
    vec2 q = abs(p - ctr) - half_size + glass_r;
    float d = length(max(q, 0.0)) + min(max(q.x, q.y), 0.0) - glass_r;
    float m = clamp(0.5 - d, 0.0, 1.0);          // anti-aliased rounded mask
    if (m <= 0.0)
        return orig;
    vec4 b = vec4(0.0);
    float w = 0.0;
    for (int i = -12; i <= 12; i++) {
        float k = exp(-float(i * i) / 72.0);
        b += k * GLASSH_tex(GLASSH_pos + vec2(0.0, float(i) * glass_step) * GLASSH_pt);
        w += k;
    }
    b /= w;
    vec3 tint = vec3(glass_tr, glass_tg, glass_tb);
    // Gentle vertical gradient in the tint, denser at the bottom.
    float t = clamp((p.y - glass_y0) / max(glass_y1 - glass_y0, 1.0), 0.0, 1.0);
    vec3 g = mix(b.rgb, tint, clamp(glass_ta * (0.9 + 0.2 * t), 0.0, 1.0));
    // 1px inner highlight along the top edge.
    float edge = clamp(1.0 - abs(d + 1.0), 0.0, 1.0) * step(p.y, ctr.y);
    g = mix(g, vec3(1.0), edge * glass_hl);
    return vec4(mix(orig.rgb, g, m), orig.a);
}
