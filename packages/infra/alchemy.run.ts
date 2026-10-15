import alchemy from "alchemy";
import { Nextjs } from "alchemy/cloudflare";
import { config } from "dotenv";

config({ path: "./.env" });
config({ path: "../../apps/web/.env" });

const app = await alchemy("Orpbal");

export const web = await Nextjs("web", {
  cwd: "../../apps/web",
  bindings: {
    NEXT_PUBLIC_SERVER_URL: alchemy.env.NEXT_PUBLIC_SERVER_URL!,
    DATABASE_URL: alchemy.secret.env.DATABASE_URL!,
    CORS_ORIGIN: alchemy.env.CORS_ORIGIN!,
  },
  dev: {
    env: {
      PORT: "3001",
    },
  },
});

console.log(`Web    -> ${web.url}`);

await app.finalize();
