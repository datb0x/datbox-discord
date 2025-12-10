import DatboxNetwork from "./network";
import DatboxConfig from "./config";
import DatboxFileSystem from "./fs";
import { server } from "..";
import RootIPC from "node-ipc";
import { name } from "../../package.json";
import type { TransferResponse } from "../types";

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

RootIPC.config.id = name;
RootIPC.serve(() => {
	RootIPC.server.on(`${name}.upload`, async (data: { localPath: string, virtualPath: string }, socket) => {
		try {
			const { path, chunks, checksum } = await datbox.uploadAsync(data.localPath, data.virtualPath, (current, total) => {
				// Send progress
				RootIPC.server.emit(socket, `${name}.response`, { data: { current, total } } as TransferResponse);
			});
			RootIPC.server.emit(socket, `${name}.response`, { text: `Uploaded to ${path} as ${chunks} chunk${chunks == 1 ? "" : "s"} (MD5 ${checksum.toString("hex")})`, final: true } as TransferResponse);
		} catch (err) {
			RootIPC.server.emit(socket, `${name}.response`, { text: `${err}`, error: true, final: true } as TransferResponse);
		}
	});
	RootIPC.server.on(`${name}.download`, async (data: { virtualPath: string, localPath: string }, socket) => {
		try {
			await datbox.downloadAsync(data.virtualPath, data.localPath, (current, total) => {
				// Send progress
				RootIPC.server.emit(socket, `${name}.response`, { data: { current, total } } as TransferResponse);
			});
			RootIPC.server.emit(socket, `${name}.response`, { text: `Downloaded to ${data.localPath} successfully`, final: true } as TransferResponse);
		} catch (err) {
			RootIPC.server.emit(socket, `${name}.response`, { text: `${err}`, error: true, final: true } as TransferResponse);
		}
	});
});

RootIPC.server.start();