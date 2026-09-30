// Sesión de la demo: el acceso se valida en el navegador y se recuerda en
// localStorage (no hay cuentas en el backend).
const KEY = "lap-session";

export function isAuthenticated(): boolean {
  try {
    return Boolean(localStorage.getItem(KEY));
  } catch {
    return false;
  }
}

export function login(username: string, password: string): boolean {
  if (username.trim().toUpperCase() !== "LAP" || password !== "LAP") return false;
  localStorage.setItem(KEY, JSON.stringify({ username: "LAP", role: "administrador", loggedAt: new Date().toISOString() }));
  return true;
}

export function logout(): void {
  localStorage.removeItem(KEY);
}
