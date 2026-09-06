export * from "./contract";
export * from "./manifest";
export * from "./client";

import type { AetherClient } from "./client";

export interface Turn {
  daemon: string;
  thread: string;
  text: string;
  client: AetherClient;
}

export interface DefineDaemonOptions {
  name: string;
  onTurn: (turn: Turn) => Promise<void>;
}

export interface DefinedDaemon extends DefineDaemonOptions {
  _brand: "aether-daemon";
}

export function defineDaemon(opts: DefineDaemonOptions): DefinedDaemon {
  return { ...opts, _brand: "aether-daemon" };
}
