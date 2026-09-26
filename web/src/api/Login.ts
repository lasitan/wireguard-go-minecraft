import { requestApi } from "./requestApi";

export async function Login(password: string): Promise<string> {
  const body = await requestApi<{ token: string }>("/api/login", {
    method: "POST",
    body: JSON.stringify({ password }),
  });
  return body.token;
}
