import { requestApi } from "./requestApi";
import { credentialToJSON, prepareCreateOptions, prepareRequestOptions } from "./webauthnCodec";

export type PasskeyInfo = {
  id: string;
  name: string;
  createdAt: string;
};

type BeginResp = {
  sessionId: string;
  options: {
    publicKey: PublicKeyCredentialCreationOptions & Record<string, unknown>;
  };
};

type BeginAssertResp = {
  sessionId: string;
  options: {
    publicKey: PublicKeyCredentialRequestOptions & Record<string, unknown>;
  };
};

export async function LoginWithPasskey(): Promise<string> {
  const begin = await requestApi<BeginAssertResp>("/api/webauthn/login/begin", { method: "POST", body: "{}" });
  const cred = (await navigator.credentials.get(prepareRequestOptions(begin.options as never))) as PublicKeyCredential | null;
  if (!cred) throw new Error("已取消通行密钥验证");
  const body = await requestApi<{ token: string }>("/api/webauthn/login/finish", {
    method: "POST",
    body: JSON.stringify({ sessionId: begin.sessionId, credential: credentialToJSON(cred) }),
  });
  return body.token;
}

export async function ListPasskeys(): Promise<PasskeyInfo[]> {
  const body = await requestApi<{ credentials: PasskeyInfo[] }>("/api/webauthn/credentials");
  return body.credentials || [];
}

export async function RegisterPasskey(name: string): Promise<PasskeyInfo> {
  const begin = await requestApi<BeginResp>("/api/webauthn/register/begin", { method: "POST", body: "{}" });
  const cred = (await navigator.credentials.create(prepareCreateOptions(begin.options as never))) as PublicKeyCredential | null;
  if (!cred) throw new Error("已取消通行密钥注册");
  return requestApi<PasskeyInfo>("/api/webauthn/register/finish", {
    method: "POST",
    body: JSON.stringify({
      sessionId: begin.sessionId,
      name,
      credential: credentialToJSON(cred),
    }),
  });
}

export async function beginPasskeyAssert(): Promise<{ sessionId: string; credential: Record<string, unknown> }> {
  const begin = await requestApi<BeginAssertResp>("/api/webauthn/assert/begin", { method: "POST", body: "{}" });
  const cred = (await navigator.credentials.get(prepareRequestOptions(begin.options as never))) as PublicKeyCredential | null;
  if (!cred) throw new Error("已取消通行密钥验证");
  return { sessionId: begin.sessionId, credential: credentialToJSON(cred) };
}

export async function RenamePasskey(
  id: string,
  name: string,
  stepUp: { password?: string; webauthnSessionId?: string; webauthnCredential?: Record<string, unknown> },
): Promise<PasskeyInfo> {
  return requestApi<PasskeyInfo>("/api/webauthn/credentials", {
    method: "PATCH",
    body: JSON.stringify({ id, name, ...stepUp }),
  });
}

export async function DeletePasskey(
  id: string,
  stepUp: { password?: string; webauthnSessionId?: string; webauthnCredential?: Record<string, unknown> },
): Promise<void> {
  await requestApi("/api/webauthn/credentials", {
    method: "DELETE",
    body: JSON.stringify({ id, ...stepUp }),
  });
}
