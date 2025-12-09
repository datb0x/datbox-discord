import DatboxNetwork from "./network";
import DatboxConfig from "./config";
import DatboxFileSystem from "./fs";
import { server } from "..";

const options = server.opts<{ channel?: string, config: string, root?: string, token?: string }>();
const config = new DatboxConfig(options.config);
config.load();

config.channelId = options.channel ?? config.channelId;
config.root = options.root ?? config.root;
config.token = options.token ?? process.env.TOKEN ?? config.token;
if (!config.channelId) throw new Error("No channel ID supplied");
if (!config.root) throw new Error("No root directory supplied");
if (!config.token) throw new Error(`No bot token supplied. Either add "TOKEN=<token>" to .env or '"token": "<token>"' to config`);

config.save();

const network = new DatboxNetwork(config.channelId);
await network.login(config.token);
const datbox = new DatboxFileSystem(config.root, network);


