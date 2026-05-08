const ss = (key: string) => ({
  get: () => sessionStorage.getItem(key),
  set: (v: string | null) => v ? sessionStorage.setItem(key, v) : sessionStorage.removeItem(key),
});
const ls = (key: string) => ({
  get: () => localStorage.getItem(key),
  set: (v: string | null) => v ? localStorage.setItem(key, v) : localStorage.removeItem(key),
});

export const accessToken  = ss('m2a_access_token');
export const refreshToken = ss('m2a_refresh_token');
export const pkceVerifier = ss('m2a_pkce_verifier');
export const oauthState   = ss('m2a_oauth_state');
export const clientId     = ls('m2a_client_id');
