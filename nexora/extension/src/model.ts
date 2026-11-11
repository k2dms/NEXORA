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
  security: 'none' | 'tls' | 'reality';
  flow: string;
  tls?: { serverName: string; alpn?: string[]; fingerprint?: string; insecure?: false };
  reality?: { serverName: string; publicKey: string; shortId?: string; fingerprint?: string; spiderX?: string };
  source: 'manual' | 'subscription';
  subscriptionId?: string;
};

export type SubscriptionFormat = 'vless-base64' | 'vless-uri-list' | 'v2ray-json' | 'clash-yaml';
export type SavedSubscription = {
  id: string;
  displayName: string;
  url: string;
  urlRef: string;
  format: SubscriptionFormat | 'auto';
  limits: { maxBytes: number; maxRedirects: number; timeoutMs: number; maxServers: number };
  keepLastGood: true;
  schedule: { intervalSeconds: number };
  status: {
    consecutiveFailures: number;
    lastAttemptAt?: string;
    lastSuccessAt?: string;
    lastSha256?: string;
    serverCount?: number;
    nextAttemptAt?: string;
    lastError?: { code: string; at?: string } | null;
  };
  serverIds: string[];
};

export type SubscriptionResult = {
  refreshed: { subscriptionId: string; sha256: string; format: SubscriptionFormat; serverCount: number; servers: SavedServer[]; lossyFields?: string[] }[];
  unchanged: string[];
  failed: { subscriptionId: string; code: string; retryable: boolean; consecutiveFailures: number; nextAttemptAt?: string | null; format?: SubscriptionFormat; rejectedCount?: number; partialServers?: SavedServer[] }[];
  skipped?: string[];
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
  servers: SavedServer[];
  subscription: { id: string; displayName: string; serverCount: number; consecutiveFailures: number; lastAttemptAt?: string; lastSuccessAt?: string; temporary: boolean } | null;
  warning: string | null;
  pending: boolean;
  error: string | null;
  coreVersion: string | null;
};

export type Action = 'status' | 'save' | 'subscribe' | 'refresh' | 'select' | 'connect' | 'disconnect' | 'open-site';
export const channel = 'nexora.connection.v1';
export const selectedKey = '-nexora.selectedProfile';
export const runtimeKey = '-nexora.runtimeConnection';
