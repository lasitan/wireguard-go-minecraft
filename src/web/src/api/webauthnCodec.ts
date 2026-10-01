function b64urlToBuf(s: string): ArrayBuffer {
  const pad = "=".repeat((4 - (s.length % 4)) % 4);
  const b64 = (s + pad).replace(/-/g, "+").replace(/_/g, "/");
  const bin = atob(b64);
  const out = new Uint8Array(bin.length);
  for (let i = 0; i < bin.length; i++) out[i] = bin.charCodeAt(i);
  return out.buffer;
}

function bufToB64url(buf: ArrayBuffer): string {
  const bytes = new Uint8Array(buf);
  let s = "";
  for (let i = 0; i < bytes.length; i++) s += String.fromCharCode(bytes[i]!);
  return btoa(s).replace(/\+/g, "-").replace(/\//g, "_").replace(/=+$/, "");
}

type ServerCreateOptions = {
  publicKey: PublicKeyCredentialCreationOptions & {
    challenge: string;
    user: Omit<PublicKeyCredentialUserEntity, "id"> & { id: string };
    excludeCredentials?: Array<Omit<PublicKeyCredentialDescriptor, "id"> & { id: string }>;
  };
};

type ServerRequestOptions = {
  publicKey: PublicKeyCredentialRequestOptions & {
    challenge: string;
    allowCredentials?: Array<Omit<PublicKeyCredentialDescriptor, "id"> & { id: string }>;
  };
};

export function passkeySupported(): boolean {
  return typeof window !== "undefined" && typeof window.PublicKeyCredential !== "undefined";
}

export function prepareCreateOptions(options: ServerCreateOptions): CredentialCreationOptions {
  const pk = options.publicKey;
  return {
    publicKey: {
      ...pk,
      challenge: b64urlToBuf(pk.challenge as unknown as string),
      user: {
        ...pk.user,
        id: b64urlToBuf(pk.user.id as unknown as string),
      },
      excludeCredentials: (pk.excludeCredentials || []).map((c) => ({
        ...c,
        id: b64urlToBuf(c.id as unknown as string),
      })),
    },
  };
}

export function prepareRequestOptions(options: ServerRequestOptions): CredentialRequestOptions {
  const pk = options.publicKey;
  return {
    publicKey: {
      ...pk,
      challenge: b64urlToBuf(pk.challenge as unknown as string),
      allowCredentials: (pk.allowCredentials || []).map((c) => ({
        ...c,
        id: b64urlToBuf(c.id as unknown as string),
      })),
    },
  };
}

export function credentialToJSON(cred: PublicKeyCredential): Record<string, unknown> {
  const anyCred = cred as PublicKeyCredential & { authenticatorAttachment?: string };
  const res = cred.response;
  const base: Record<string, unknown> = {
    id: cred.id,
    rawId: bufToB64url(cred.rawId),
    type: cred.type,
    clientExtensionResults: cred.getClientExtensionResults(),
  };
  if (anyCred.authenticatorAttachment) {
    base.authenticatorAttachment = anyCred.authenticatorAttachment;
  }
  if (res instanceof AuthenticatorAttestationResponse) {
    const att = res as AuthenticatorAttestationResponse & { getTransports?: () => string[] };
    base.response = {
      clientDataJSON: bufToB64url(res.clientDataJSON),
      attestationObject: bufToB64url(res.attestationObject),
      transports: att.getTransports?.() || [],
    };
  } else {
    const assertion = res as AuthenticatorAssertionResponse;
    base.response = {
      clientDataJSON: bufToB64url(assertion.clientDataJSON),
      authenticatorData: bufToB64url(assertion.authenticatorData),
      signature: bufToB64url(assertion.signature),
      userHandle: assertion.userHandle ? bufToB64url(assertion.userHandle) : null,
    };
  }
  return base;
}
