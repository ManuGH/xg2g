// Legacy AuthService compatibility wrapper
import * as api from '../../client-ts';

export const AuthService = {
  createSession: api.createSession,
};
