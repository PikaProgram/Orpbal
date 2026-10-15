import axios from "axios";

const serverBaseURL = "/api";

export function createHttpClient(baseURL = serverBaseURL) {
  return axios.create({
    baseURL,
    headers: { "Content-Type": "application/json" },
    timeout: 10_000,
  });
}

export const httpClient = createHttpClient();
