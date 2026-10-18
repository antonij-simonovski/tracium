import { BaseAPIClient } from './client';

export class UsersAPI extends BaseAPIClient {
  async listUsers(): Promise<{ id: string; name: string }[]> {
    return this.get('/users');
  }

  /** Changes the signed-in user's password and returns the new session token;
   * the old one stops working. Throws APIError(401) when currentPassword doesn't
   * match, or APIError(400) for a weak newPassword. */
  async changePassword(currentPassword: string, newPassword: string): Promise<string> {
    const { token } = await this.post<{ token: string }>('/auth/password', { current_password: currentPassword, new_password: newPassword });
    return token;
  }
}
