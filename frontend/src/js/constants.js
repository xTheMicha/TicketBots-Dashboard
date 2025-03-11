export const FRONTPAGE_URL = env.FRONTPAGE_URL || "https://ticketsbot.cloud";
export const API_URL = env.API_URL || "http://localhost:8081";
export const WS_URL = env.WS_URL || "ws://localhost:8081";
export const DOCS_URL = env.DOCS_URL || "https://docs.ticketsbot.cloud";
export const TITLE = env.TITLE || "Tickets | A Discord Support Manager Bot";
export const DESCRIPTION = env.DESCRIPTION || "Management panel for the Discord Tickets bot";
export const FAVICON = env.FAVICON || "/favicon.ico";
export const FAVICON_TYPE = env.FAVICON_TYPE || "image/ico";
export const WHITELABEL_DISABLED = env.WHITELABEL_DISABLED || false;

export const OAUTH = {
  clientId: env.CLIENT_ID || "1325579039888511056",
  redirectUri: env.REDIRECT_URI || "http://localhost:5000/callback",
};
