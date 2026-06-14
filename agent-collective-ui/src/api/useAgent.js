// src/api/useAgent.js
export function useAgent(endpoint) {
  let data = null;
  let loading = false;
  let error = null;

  async function call(payload) {
    loading = true;
    error = null;
    try {
      const res = await fetch(`http://localhost:5000/api/${endpoint}`, {
        method: "POST",
        headers: { "Content-Type": "application/json" },
        body: JSON.stringify(payload)
      });
      const json = await res.json();
      if (!res.ok) throw new Error(json.error || "unknown error");
      data = json;
    } catch (e) {
      error = e.message;
    } finally {
      loading = false;
    }
  }

  return { data, loading, error, call };
}
