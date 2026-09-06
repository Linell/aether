/**
 * Glyph atlas for the condensed state: a single row of terminal characters
 * drawn once into a 2D canvas and uploaded as a texture. The fragment shader
 * indexes it by column, so the atlas layout is `GLYPHS.length` square cells.
 */

export const GLYPHS = '01$>_~/\\|:.aethr';
export const ATLAS_CELL = 32;

/**
 * Draws the glyph set in JetBrains Mono (or the closest monospace available).
 * Waits for web fonts to settle so the atlas isn't rasterised with a fallback
 * face mid-swap. Returns null if 2D canvas is unavailable.
 */
export async function drawGlyphAtlas(): Promise<HTMLCanvasElement | null> {
  const atlas = document.createElement('canvas');
  atlas.width = ATLAS_CELL * GLYPHS.length;
  atlas.height = ATLAS_CELL;

  const ctx = atlas.getContext('2d');
  if (!ctx) return null;

  const font = `400 ${Math.round(ATLAS_CELL * 0.72)}px "JetBrains Mono", ui-monospace, monospace`;
  if ('fonts' in document) {
    try {
      await document.fonts.load(font);
      await document.fonts.ready;
    } catch {
      // Font loading failed; the generic monospace fallback is acceptable.
    }
  }

  // Opaque black ground with white ink so the shader can read a clean red
  // channel without premultiplied-alpha fringing at the edges.
  ctx.fillStyle = '#000';
  ctx.fillRect(0, 0, atlas.width, atlas.height);
  ctx.fillStyle = '#fff';
  ctx.font = font;
  ctx.textAlign = 'center';
  ctx.textBaseline = 'middle';
  for (let i = 0; i < GLYPHS.length; i++) {
    ctx.fillText(GLYPHS.charAt(i), (i + 0.5) * ATLAS_CELL, ATLAS_CELL * 0.5);
  }
  return atlas;
}

/**
 * Fills `texture` with the atlas, or with a single black pixel when no atlas
 * is available yet. A black texture reads as zero glyph coverage, so nothing
 * shows until the real atlas lands.
 */
export function uploadGlyphAtlas(
  gl: WebGL2RenderingContext,
  texture: WebGLTexture,
  atlas: HTMLCanvasElement | null,
): void {
  gl.bindTexture(gl.TEXTURE_2D, texture);
  gl.texParameteri(gl.TEXTURE_2D, gl.TEXTURE_WRAP_S, gl.CLAMP_TO_EDGE);
  gl.texParameteri(gl.TEXTURE_2D, gl.TEXTURE_WRAP_T, gl.CLAMP_TO_EDGE);
  gl.texParameteri(gl.TEXTURE_2D, gl.TEXTURE_MAG_FILTER, gl.LINEAR);

  if (atlas) {
    gl.pixelStorei(gl.UNPACK_FLIP_Y_WEBGL, true);
    gl.texImage2D(gl.TEXTURE_2D, 0, gl.R8, gl.RED, gl.UNSIGNED_BYTE, atlas);
    gl.pixelStorei(gl.UNPACK_FLIP_Y_WEBGL, false);
    gl.generateMipmap(gl.TEXTURE_2D);
    gl.texParameteri(gl.TEXTURE_2D, gl.TEXTURE_MIN_FILTER, gl.LINEAR_MIPMAP_LINEAR);
  } else {
    gl.texImage2D(gl.TEXTURE_2D, 0, gl.R8, 1, 1, 0, gl.RED, gl.UNSIGNED_BYTE, new Uint8Array([0]));
    gl.texParameteri(gl.TEXTURE_2D, gl.TEXTURE_MIN_FILTER, gl.LINEAR);
  }
}
