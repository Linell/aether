#version 300 es
precision highp float;

in vec2 v_uv;
out vec4 outColor;

uniform vec2  u_resolution;
uniform float u_time;
uniform vec2  u_pointer;     // 0..1, origin bottom-left
uniform float u_stir;        // 0..1 decayed scroll velocity
uniform float u_dim;         // brightness multiplier for quieter instances
uniform float u_cell;        // glyph cell size in render pixels
uniform float u_glyphCount;  // glyphs in the single-row atlas
uniform float u_glyphLod;    // mip level matching u_cell against the atlas
uniform sampler2D u_glyphs;

// --- Palette ----------------------------------------------------------
const vec3 INK   = vec3(0.027, 0.031, 0.039);  // #07080a
const vec3 SMOKE = vec3(0.541, 0.580, 0.651);  // cold grey-blue
const vec3 EMBER = vec3(0.780, 0.470, 0.290);  // warm, used sparingly

// --- 3D simplex noise (Ashima / McEwan), public domain -------------------

vec3 mod289(vec3 x) { return x - floor(x * (1.0 / 289.0)) * 289.0; }
vec4 mod289(vec4 x) { return x - floor(x * (1.0 / 289.0)) * 289.0; }
vec4 permute(vec4 x) { return mod289(((x * 34.0) + 1.0) * x); }
vec4 taylorInvSqrt(vec4 r) { return 1.79284291400159 - 0.85373472095314 * r; }

float snoise(vec3 v) {
  const vec2 C = vec2(1.0 / 6.0, 1.0 / 3.0);
  const vec4 D = vec4(0.0, 0.5, 1.0, 2.0);

  vec3 i  = floor(v + dot(v, C.yyy));
  vec3 x0 = v - i + dot(i, C.xxx);

  vec3 g  = step(x0.yzx, x0.xyz);
  vec3 l  = 1.0 - g;
  vec3 i1 = min(g.xyz, l.zxy);
  vec3 i2 = max(g.xyz, l.zxy);

  vec3 x1 = x0 - i1 + C.xxx;
  vec3 x2 = x0 - i2 + C.yyy;
  vec3 x3 = x0 - D.yyy;

  i = mod289(i);
  vec4 p = permute(permute(permute(
      i.z + vec4(0.0, i1.z, i2.z, 1.0))
    + i.y + vec4(0.0, i1.y, i2.y, 1.0))
    + i.x + vec4(0.0, i1.x, i2.x, 1.0));

  float n_ = 0.142857142857;
  vec3 ns = n_ * D.wyz - D.xzx;

  vec4 j = p - 49.0 * floor(p * ns.z * ns.z);
  vec4 x_ = floor(j * ns.z);
  vec4 y_ = floor(j - 7.0 * x_);

  vec4 x = x_ * ns.x + ns.yyyy;
  vec4 y = y_ * ns.x + ns.yyyy;
  vec4 h = 1.0 - abs(x) - abs(y);

  vec4 b0 = vec4(x.xy, y.xy);
  vec4 b1 = vec4(x.zw, y.zw);

  vec4 s0 = floor(b0) * 2.0 + 1.0;
  vec4 s1 = floor(b1) * 2.0 + 1.0;
  vec4 sh = -step(h, vec4(0.0));

  vec4 a0 = b0.xzyw + s0.xzyw * sh.xxyy;
  vec4 a1 = b1.xzyw + s1.xzyw * sh.zzww;

  vec3 p0 = vec3(a0.xy, h.x);
  vec3 p1 = vec3(a0.zw, h.y);
  vec3 p2 = vec3(a1.xy, h.z);
  vec3 p3 = vec3(a1.zw, h.w);

  vec4 norm = taylorInvSqrt(vec4(dot(p0, p0), dot(p1, p1), dot(p2, p2), dot(p3, p3)));
  p0 *= norm.x; p1 *= norm.y; p2 *= norm.z; p3 *= norm.w;

  vec4 m = max(0.6 - vec4(dot(x0, x0), dot(x1, x1), dot(x2, x2), dot(x3, x3)), 0.0);
  m = m * m;
  return 42.0 * dot(m * m, vec4(dot(p0, x0), dot(p1, x1), dot(p2, x2), dot(p3, x3)));
}

// --- Fractal Brownian motion -------------------------------------------

float fbm(vec3 p) {
  float sum = 0.0;
  float amp = 0.5;
  for (int i = 0; i < 4; i++) {
    sum += amp * snoise(p);
    p = p * 2.02 + vec3(17.0, 31.0, 7.0);
    amp *= 0.5;
  }
  return sum;
}

// --- Hash ------------------------------------------------------------

float hash(vec2 p) {
  return fract(sin(dot(p, vec2(127.1, 311.7))) * 43758.5453);
}

// --- Smoke -------------------------------------------------------------

// Smoke density at a point. `condense` tightens the remap so the field reads
// sharper as it hardens into glyphs.
float smokeDensity(vec2 uv, float aspect, float condense) {
  vec2 p = vec2(uv.x * aspect, uv.y);

  float t = u_time * 0.035;

  // Slow rise: smoke drifts upward, so sample lower in the field as time passes.
  vec3 q = vec3(p * 1.05, t);
  q.y -= t * 0.7;

  // Two rounds of domain warping give the folding, curling look.
  vec2 warp1 = vec2(fbm(q + vec3(0.0, 0.0, 1.7)), fbm(q + vec3(5.2, 1.3, 3.1)));
  vec2 warp2 = vec2(
    fbm(q + vec3(warp1 * 0.9, 0.0) + vec3(1.7, 9.2, 0.6)),
    fbm(q + vec3(warp1 * 0.9, 0.0) + vec3(8.3, 2.8, 2.4))
  );
  float density = fbm(q + vec3(warp2 * 0.7, 0.0));

  // Remap to 0..1 and shape it so most of the frame stays near-black.
  density = smoothstep(-0.45 + 0.15 * condense, 0.95 - 0.35 * condense, density);

  // Denser toward the bottom, thinning as it rises.
  float rise = smoothstep(1.15, -0.1, uv.y);
  density *= mix(0.35, 1.0, rise);

  // Pointer: a faint clearing where the cursor is, like a hand parting fog.
  vec2 toPointer = (uv - u_pointer) * vec2(aspect, 1.0);
  float clearing = 1.0 - 0.45 * exp(-dot(toPointer, toPointer) * 9.0);
  density *= clearing;

  // Vignette keeps the edges settled.
  vec2 centered = uv - 0.5;
  float vignette = 1.0 - smoothstep(0.35, 0.95, length(centered * vec2(1.0, 1.35)));
  density *= vignette;

  // Scrolling stirs the smoke: a touch thicker while it moves.
  density *= 1.0 + 0.25 * u_stir;

  return density * u_dim;
}

// --- Glyphs ------------------------------------------------------------

// Falling terminal glyphs, quantised to a character grid. Each column drifts
// down at its own slow pace; a cell only lights where the smoke is dense.
vec3 glyphLayer(vec2 uv, float aspect, float condense) {
  vec2 cellPos = gl_FragCoord.xy / u_cell;
  vec2 cell = floor(cellPos);
  vec2 cellUv = fract(cellPos);
  vec2 cellCenter = (cell + 0.5) * u_cell / u_resolution;

  float density = smokeDensity(cellCenter, aspect, condense);

  // Column rhythm: offset and speed differ per column, rows per second.
  float offset = hash(vec2(cell.x, 0.0)) * 37.0;
  float speed = 0.4 + 0.9 * hash(vec2(cell.x, 1.0));
  float fall = u_time * speed + offset;
  float row = floor(fall);
  float phase = fract(fall);

  // Keying on (column, row + step) shifts the pattern down one cell per step.
  vec2 key = vec2(cell.x, cell.y + row);
  float index = floor(hash(key) * u_glyphCount);
  float gate = step(0.5, hash(key + 11.0));
  float brightness = smoothstep(0.06, 0.5, density) * mix(0.5, 1.0, hash(key + 23.0));
  brightness *= 0.7 + 0.3 * smoothstep(0.0, 0.3, phase);

  vec2 atlasUv = vec2((index + cellUv.x) / u_glyphCount, cellUv.y);
  float glyph = textureLod(u_glyphs, atlasUv, u_glyphLod).r;

  // The rare ember glyph, kept low in the frame.
  float ember = step(0.97, hash(key + 41.0)) * smoothstep(0.5, 0.0, uv.y);
  vec3 tint = mix(SMOKE, EMBER, ember);

  return tint * glyph * gate * brightness;
}

// --- Compose -----------------------------------------------------------

void main() {
  float aspect = u_resolution.x / u_resolution.y;
  vec2 uv = v_uv;

  // Breathing: a slow drifting field decides where the smoke crystallizes into
  // glyphs and melts back. The global phase starts at 0 so the page loads as
  // near-pure smoke, then breathes in over ~28s cycles.
  float breathe = 0.5 - 0.5 * cos(u_time * 0.22 + 0.9);
  float crystal = 0.5 + 0.5 * fbm(vec3(uv * vec2(aspect, 1.0) * 1.3, u_time * 0.04 + 40.0));
  float condense = smoothstep(0.38, 0.65, crystal + (breathe - 0.5) * 0.9);
  float density = smokeDensity(uv, aspect, condense);

  // Color: cold smoke over ink, with a trace of ember low in the frame.
  float emberMask = smoothstep(0.55, 0.0, uv.y) * smoothstep(0.2, 0.75, density) * 0.18;
  vec3 smokeColor = mix(SMOKE, EMBER, emberMask);
  vec3 color = mix(INK, smokeColor, density * 0.34);

  // Condensation: the smoke thins to a haze and the glyphs take its place.
  // Where nothing is condensing the glyph pass is skipped.
  if (condense > 0.001) {
    vec3 condensed = mix(INK, smokeColor, density * 0.34 * 0.5);
    condensed += glyphLayer(uv, aspect, condense);
    color = mix(color, condensed, condense);
  }

  // Subtle dither hides banding in the dark gradients.
  float dither = fract(sin(dot(gl_FragCoord.xy, vec2(12.9898, 78.233))) * 43758.5453) / 255.0;
  color += dither;

  outColor = vec4(color, 1.0);
}
