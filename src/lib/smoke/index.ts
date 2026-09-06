import vertexSource from './smoke.vert?raw';
import fragmentSource from './smoke.frag?raw';
import { createProgram, getUniform } from './gl';
import { ATLAS_CELL, GLYPHS, drawGlyphAtlas, uploadGlyphAtlas } from './glyphs';

export interface SmokeOptions {
  /**
   * Render scale relative to CSS pixels. Smoke is soft, so rendering at
   * half resolution is visually indistinguishable and four times cheaper.
   */
  scale?: number;
  /** Multiplies final density and brightness. 0.5 reads as a quieter backdrop. */
  dim?: number;
}

/** Stops the render loop and releases GL resources. */
export type Dispose = () => void;

/** Glyph grid cell size in CSS pixels. */
const GLYPH_CELL_CSS = 16;

/** Upper bound on frame delta so a paused loop doesn't lurch when it resumes. */
const MAX_DT = 0.1;

interface Resources {
  program: WebGLProgram;
  vao: WebGLVertexArrayObject;
  texture: WebGLTexture;
  uniforms: {
    resolution: WebGLUniformLocation;
    time: WebGLUniformLocation;
    pointer: WebGLUniformLocation;
    stir: WebGLUniformLocation;
    dim: WebGLUniformLocation;
    cell: WebGLUniformLocation;
    glyphCount: WebGLUniformLocation;
    glyphLod: WebGLUniformLocation;
    glyphs: WebGLUniformLocation;
  };
}

function createResources(gl: WebGL2RenderingContext, atlas: HTMLCanvasElement | null): Resources {
  const program = createProgram(gl, vertexSource, fragmentSource);
  gl.useProgram(program);

  // Full-screen triangle needs a bound VAO in WebGL2 but no buffers.
  const vao = gl.createVertexArray();
  if (!vao) throw new Error('Could not create vertex array');
  gl.bindVertexArray(vao);

  const texture = gl.createTexture();
  if (!texture) throw new Error('Could not create texture');
  gl.activeTexture(gl.TEXTURE0);
  uploadGlyphAtlas(gl, texture, atlas);

  return {
    program,
    vao,
    texture,
    uniforms: {
      resolution: getUniform(gl, program, 'u_resolution'),
      time: getUniform(gl, program, 'u_time'),
      pointer: getUniform(gl, program, 'u_pointer'),
      stir: getUniform(gl, program, 'u_stir'),
      dim: getUniform(gl, program, 'u_dim'),
      cell: getUniform(gl, program, 'u_cell'),
      glyphCount: getUniform(gl, program, 'u_glyphCount'),
      glyphLod: getUniform(gl, program, 'u_glyphLod'),
      glyphs: getUniform(gl, program, 'u_glyphs'),
    },
  };
}

function deleteResources(gl: WebGL2RenderingContext, res: Resources): void {
  gl.deleteTexture(res.texture);
  gl.deleteVertexArray(res.vao);
  gl.deleteProgram(res.program);
}

function clamp01(value: number): number {
  return Math.min(1, Math.max(0, value));
}

/**
 * Mounts the smoke shader onto a canvas.
 *
 * - Renders at reduced resolution and never above device pixel ratio 1.
 * - Pauses when the tab is hidden or the canvas leaves the viewport.
 * - Renders a single still frame when the user prefers reduced motion.
 * - Breathes between smoke and falling terminal glyphs on a slow cycle.
 * - Recovers from WebGL context loss by rebuilding its resources.
 * - Returns null when WebGL2 is unavailable so the caller can fall back to CSS.
 */
export function mountSmoke(canvas: HTMLCanvasElement, options: SmokeOptions = {}): Dispose | null {
  const gl = canvas.getContext('webgl2', {
    alpha: false,
    antialias: false,
    depth: false,
    stencil: false,
    powerPreference: 'low-power',
  });
  if (!gl) return null;
  return start(canvas, gl, options);
}

/** Runs the smoke on an acquired context; everything from here on has a live `gl`. */
function start(canvas: HTMLCanvasElement, gl: WebGL2RenderingContext, options: SmokeOptions): Dispose {
  const scale = options.scale ?? 0.5;
  const dim = options.dim ?? 1;

  let atlas: HTMLCanvasElement | null = null;
  let res = createResources(gl, atlas);
  let disposed = false;

  // Pointer eases toward its target so the clearing in the fog feels weighty.
  const pointer = { x: 0.5, y: 0.6 };
  const pointerTarget = { x: 0.5, y: 0.6 };

  // Stir is decayed scroll velocity.
  let stir = 0;
  let lastScrollY = window.scrollY;

  // Noise time accumulates at a rate stirred by scrolling, so speeding up
  // never causes the field to jump.
  let time = 0;
  let last = 0;

  const reducedMotion = window.matchMedia('(prefers-reduced-motion: reduce)').matches;
  let visible = true;
  let frame = 0;

  function cellSize(): number {
    return GLYPH_CELL_CSS * Math.min(window.devicePixelRatio, 1) * scale;
  }

  /** Uniforms that only change on (re)initialisation or resize. */
  function setStaticUniforms(): void {
    const { uniforms } = res;
    const cell = cellSize();
    gl.viewport(0, 0, canvas.width, canvas.height);
    gl.uniform2f(uniforms.resolution, canvas.width, canvas.height);
    gl.uniform1f(uniforms.dim, dim);
    gl.uniform1f(uniforms.cell, cell);
    gl.uniform1f(uniforms.glyphCount, GLYPHS.length);
    gl.uniform1f(uniforms.glyphLod, Math.max(0, Math.log2(ATLAS_CELL / cell)));
    gl.uniform1i(uniforms.glyphs, 0);
  }

  function resize(): void {
    const dpr = Math.min(window.devicePixelRatio, 1);
    const width = Math.max(1, Math.floor(canvas.clientWidth * dpr * scale));
    const height = Math.max(1, Math.floor(canvas.clientHeight * dpr * scale));
    if (canvas.width !== width || canvas.height !== height) {
      canvas.width = width;
      canvas.height = height;
      setStaticUniforms();
    }
  }

  function draw(now: number): void {
    const dt = last === 0 ? 0 : Math.min(MAX_DT, (now - last) / 1000);
    last = now;

    pointer.x += (pointerTarget.x - pointer.x) * 0.04;
    pointer.y += (pointerTarget.y - pointer.y) * 0.04;

    // Scroll velocity feeds the stir; it decays over roughly a quarter second.
    const scrollY = window.scrollY;
    const delta = Math.abs(scrollY - lastScrollY);
    lastScrollY = scrollY;
    stir = clamp01(stir * Math.exp(-dt * 4) + delta / 500);

    time += dt * (1 + 1.5 * stir);

    const { uniforms } = res;
    gl.uniform1f(uniforms.time, time);
    gl.uniform2f(uniforms.pointer, pointer.x, pointer.y);
    gl.uniform1f(uniforms.stir, stir);
    gl.drawArrays(gl.TRIANGLES, 0, 3);
  }

  function loop(now: number): void {
    draw(now);
    frame = requestAnimationFrame(loop);
  }

  function setRunning(shouldRun: boolean): void {
    const running = frame !== 0;
    if (shouldRun && !running) {
      last = 0;
      frame = requestAnimationFrame(loop);
    } else if (!shouldRun && running) {
      cancelAnimationFrame(frame);
      frame = 0;
    }
  }

  function updateRunning(): void {
    setRunning(visible && !document.hidden && !reducedMotion && !gl.isContextLost());
  }

  const onPointerMove = (event: PointerEvent): void => {
    const rect = canvas.getBoundingClientRect();
    pointerTarget.x = (event.clientX - rect.left) / rect.width;
    pointerTarget.y = 1 - (event.clientY - rect.top) / rect.height;
  };

  const onResize = (): void => {
    resize();
    if (reducedMotion) draw(performance.now());
  };

  const onContextLost = (event: Event): void => {
    event.preventDefault();
    setRunning(false);
  };

  const onContextRestored = (): void => {
    res = createResources(gl, atlas);
    setStaticUniforms();
    if (reducedMotion) {
      draw(performance.now());
    } else {
      updateRunning();
    }
  };

  const observer = new IntersectionObserver(([entry]) => {
    visible = entry?.isIntersecting ?? true;
    updateRunning();
  });

  resize();
  setStaticUniforms();
  observer.observe(canvas);
  window.addEventListener('resize', onResize);
  window.addEventListener('pointermove', onPointerMove, { passive: true });
  document.addEventListener('visibilitychange', updateRunning);
  canvas.addEventListener('webglcontextlost', onContextLost);
  canvas.addEventListener('webglcontextrestored', onContextRestored);

  void drawGlyphAtlas().then((drawn) => {
    if (disposed || !drawn) return;
    atlas = drawn;
    if (!gl.isContextLost()) uploadGlyphAtlas(gl, res.texture, atlas);
  });

  if (reducedMotion) {
    draw(performance.now());
  } else {
    updateRunning();
  }

  return () => {
    disposed = true;
    setRunning(false);
    observer.disconnect();
    window.removeEventListener('resize', onResize);
    window.removeEventListener('pointermove', onPointerMove);
    document.removeEventListener('visibilitychange', updateRunning);
    canvas.removeEventListener('webglcontextlost', onContextLost);
    canvas.removeEventListener('webglcontextrestored', onContextRestored);
    deleteResources(gl, res);
  };
}
