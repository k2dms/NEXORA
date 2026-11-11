export type ServerForm = {
  displayName: string;
  address: string;
  port: number;
  userId: string;
  serverName: string;
  publicKey: string;
  shortId: string;
  flow: boolean;
};

export type SavedServer = {
  id: string;
  displayName: string;
  protocol: 'vless';
  address: string;
  port: number;
  userIdRef: string;
  network: 'tcp';
  security: 'reality';
  flow: '' | 'xtls-rprx-vision';
  reality: { serverName: string; publicKey: string; shortId: string; fingerprint: string };
  source: 'manual';
};

export type SavedProfile = {
  id: string;
  displayName: string;
  kind: 'connector';
  serverIds: string[];
  defaultServerId: string;
  selection: { mode: 'fixed' };
  failover: { enabled: false };
  failurePolicy: { coreDeath: { action: 'block' } };
};

export type ConnectionView = {
  phase: 'disconnected' | 'connecting' | 'connected' | 'blocked' | 'disconnecting';
  connector: 'idle' | 'connecting' | 'ready' | 'waiting' | 'incompatible' | 'stopped';
  server: Pick<SavedServer, 'id' | 'displayName' | 'address' | 'port'> | null;
  error: string | null;
  coreVersion: string | null;
};

export type Action = 'status' | 'save' | 'connect' | 'disconnect' | 'open-site';
export const channel = 'nexora.connection.v1';
export const selectedKey = '-nexora.selectedProfile';
export const runtimeKey = '-nexora.runtimeConnection';
