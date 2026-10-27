import { publicRequest } from '../auth/api';

export interface OAuthClientInfo {
  client_name: string;
  redirect_uri: string;
}

const authHeader = (token: string) => ({ Authorization: `Bearer ${token}` });

export async function oauthClientInfo(token: string, params: URLSearchParams): Promise<OAuthClientInfo> {
  const query = new URLSearchParams({ client_id: params.get('client_id') ?? '', redirect_uri: params.get('redirect_uri') ?? '' });
  const response = await publicRequest(`/v1/oauth/authorize?${query}`, { headers: authHeader(token) }, 'Could not load this app');
  return response.json() as Promise<OAuthClientInfo>;
}

export async function decideOAuth(token: string, params: URLSearchParams, approve: boolean): Promise<string> {
  const field = (name: string) => params.get(name) ?? '';
  const response = await publicRequest('/v1/oauth/authorize', {
    method: 'POST',
    headers: { ...authHeader(token), 'Content-Type': 'application/json' },
    body: JSON.stringify({
      client_id: field('client_id'),
      redirect_uri: field('redirect_uri'),
      response_type: field('response_type'),
      state: field('state'),
      code_challenge: field('code_challenge'),
      code_challenge_method: field('code_challenge_method'),
      resource: field('resource'),
      approve,
    }),
  }, 'Could not connect this app');
  const body = await response.json() as { redirect_to: string };
  return body.redirect_to;
}
