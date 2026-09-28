import { useEffect, useReducer } from "react";
import { TOKEN_KEY } from "../../core/constants";
import { demoMesh, demoMeta } from "../../core/demoData";
import { state, subscribe } from "../../core/state";
import { refresh, startPoll } from "../../app/Session";
import { hydrateNodePositions } from "../../topology/NodePositionStore";
import { LoginPage } from "../login/LoginPage";
import { TopologyPage } from "../topology/TopologyPage";

export function AppRoot() {
  const [, bump] = useReducer((n: number) => n + 1, 0);
  useEffect(() => subscribe(bump), []);

  useEffect(() => {
    if (state.demo) {
      if (!state.mesh) state.mesh = demoMesh();
      if (!state.meta) state.meta = demoMeta();
      hydrateNodePositions(state.mesh);
      startPoll();
      bump();
      return;
    }
    if (state.token && !state.mesh) {
      refresh()
        .then(() => startPoll())
        .catch((e) => {
          state.token = "";
          localStorage.removeItem(TOKEN_KEY);
          state.err = e.message;
          bump();
        });
    }
  }, []);

  if (state.demo) {
    if (!state.mesh) {
      state.mesh = demoMesh();
      hydrateNodePositions(state.mesh);
    }
    if (!state.meta) state.meta = demoMeta();
    return <TopologyPage />;
  }

  if (!state.token) return <LoginPage />;

  if (!state.mesh) {
    return (
      <main className="login-wrap">
        <p className="muted">加载中…</p>
      </main>
    );
  }

  return <TopologyPage />;
}
