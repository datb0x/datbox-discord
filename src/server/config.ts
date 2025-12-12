import * as fs from "fs";
import * as path from "path";
import { name } from "../../package.json";
import envPaths from "env-paths";

const paths = envPaths(name, { suffix: "" });

export default class DatboxConfig {
	readonly configPath: string;
	channelId?: string;
	dataDir?: string;
	concurrency?: number;
	token?: string;

	constructor(configPath: string) {
		this.configPath = configPath;
	}

	load() {
		if (fs.existsSync(this.configPath)) {
			try {
				const data = JSON.parse(fs.readFileSync(this.configPath, "utf8"));
				if (typeof data?.channelId == "string") this.channelId = data.channelId;
				if (typeof data?.dataDir == "string") this.dataDir = data.dataDir;
				if (typeof data?.concurrency == "number") this.concurrency = data.concurrency;
				if (typeof data?.token == "string") this.token = data.token;
			} catch (err) {
				console.error("Failed to parse config file");
			}
		} else {
			console.log("Configuration not found. Default settings will be used, except for channelId");
			this.dataDir = paths.config;
		}
	}

	save() {
		if (!fs.existsSync(this.configPath))
			fs.mkdirSync(path.dirname(this.configPath), { recursive: true });
		fs.writeFileSync(this.configPath, JSON.stringify({
			channelId: this.channelId,
			dataDir: this.dataDir,
			concurrency: this.concurrency,
			token: this.token
		}));
	}
}