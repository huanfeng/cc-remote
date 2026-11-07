// Kept apart from backend.ts (which pulls in React) so the HTTP backend and its
// node tests can load it as plain TypeScript.

/** What became of a steer the host could not confirm (POST/GET /steer). */
export type Delivery = 'rejected' | 'pending' | 'unknown';

/**
 * Every Backend rejects with this. `status` is the host's HTTP status (409 needs
 * confirmation or is already handled, 428 trust-required, 401 signed out…), 0
 * when the host could not be reached; `code` and `delivery` carry the host's
 * extra fields when present.
 */
export class BackendError extends Error {
  readonly status: number;
  readonly code?: string;
  readonly delivery?: Delivery;
  constructor(message: string, status: number, code?: string, delivery?: Delivery) {
    super(message);
    this.name = 'BackendError';
    this.status = status;
    this.code = code;
    this.delivery = delivery;
  }
}
