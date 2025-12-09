import * as fs from "fs";
import * as path from "path";
import { name } from "../../package.json";
import envPaths from "env-paths";

const paths = envPaths(name, { suffix: "" });

export default class DatboxConfig {
	readonly configPath: string;
	channelId?: string;
	root?: string;
	token?: string;

	constructor(configPath: string) {
		this.configPath = configPath;
	}

	load() {
		if (fs.existsSync(this.configPath)) {
			try {
				const data = JSON.parse(fs.readFileSync(this.configPath, "utf8"));
				if (typeof data?.channelId == "string") this.channelId = data.channelId;
				if (typeof data?.root == "string") this.root = data.root;
				if (typeof data?.token == "string") this.token = data.token;
			} catch (err) {
				console.error("Failed to parse config file");
			}
		} else {
			console.log("Configuration not found. Default settings will be used, except for channelId");
			this.root = path.join(paths.config, "data");
		}
	}

	save() {
		if (!fs.existsSync(this.configPath))
			fs.mkdirSync(path.dirname(this.configPath), { recursive: true });
		fs.writeFileSync(this.configPath, JSON.stringify({
			channelId: this.channelId,
			root: this.root,
			token: this.token
		}));
	}
}