import DatboxNetwork from "./network";
import DatboxConfig from "./config";
import DatboxFileSystem from "./fs";
import { server } from "..";
import RootIPC from "node-ipc";
import { name } from "../../package.json";
import type { NoDataResponse, TransferResponse } from "../types";
import type { Stats } from "fs";
import * as path from "path";

const options = server.opts<{ channel?: string, config: string, dataDir?: string, concurrency: number, token?: string }>();
const config = new DatboxConfig(options.config);
config.load();

config.channelId = options.channel ?? config.channelId;
config.dataDir = options.dataDir ?? config.dataDir;
config.concurrency = options.concurrency ?? config.concurrency;
config.token = options.token ?? process.env.TOKEN ?? config.token;
if (!config.channelId) throw new Error("No channel ID supplied");
if (!config.dataDir) throw new Error("No data directory supplied");
if (!config.concurrency) throw new Error("Concurrency must be larger than 0");
if (!config.token) throw new Error(`No bot token supplied. Either add "TOKEN=<token>" to .env or '"token": "<token>"' to config`);

config.save();

const network = new DatboxNetwork(config.channelId);
await network.login(config.token);
const datbox = new DatboxFileSystem(config.dataDir, config.concurrency, network);

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
	RootIPC.server.on(`${name}.list`, async (data: { virtualPath?: string, long: boolean }, socket) => {
		try {
			const results = await datbox.readdirAsync(data.virtualPath || "/", data.long);
			let body = "";
			if (data.long) {
				const months = ["Jan", "Feb", "Mar", "Apr", "May", "Jun", "Jul", "Aug", "Sep", "Oct", "Nov", "Dec"];
				const byteStrLen = results.map(entry => entry.stat.size.toString().length).reduce((a, b) => Math.max(a, b));
				results.forEach((entry, ii) => {
					if (ii != 0) body += "\n";
					body += entry.stat.size.toString().padStart(byteStrLen, " ");
					const date = entry.stat.mtime;
					body += " " + months[date.getMonth()];
					body += " " + date.getDate().toString().padStart(2, " ");
					if (date.getFullYear() < new Date().getFullYear()) body += "  " + date.getFullYear();
					else body += " " + date.getHours().toString().padStart(2, "0") + ":" + date.getMinutes().toString().padStart(2, "0");
					if (entry.stat.isDirectory()) body += " " + entry.name + "/";
					else body += " " + entry.name;
				});
			} else results.forEach((entry) => {
				if (entry.stat.isDirectory()) body += entry.name + "/\t";
				else body += entry.name + "\t";
			});
			RootIPC.server.emit(socket, `${name}.response`, { text: body, final: true } as NoDataResponse);
		} catch (err) {
			RootIPC.server.emit(socket, `${name}.response`, { text: `${err}`, error: true, final: true } as NoDataResponse);
		}
	});
	RootIPC.server.on(`${name}.move`, async (data: { src: string, dest: string }, socket) => {
		try {
			datbox.moveSync(data.src, data.dest);
			RootIPC.server.emit(socket, `${name}.response`, { final: true } as NoDataResponse);
		} catch (err) {
			RootIPC.server.emit(socket, `${name}.response`, { text: `${err}`, error: true, final: true } as NoDataResponse);
		}
	});
	RootIPC.server.on(`${name}.remove`, async (data: { virtualPath: string, recursive?: boolean, remote?: boolean }, socket) => {
		try {
			await datbox.rmAsync(data.virtualPath, { recursive: data.recursive, remote: data.remote });
			RootIPC.server.emit(socket, `${name}.response`, { final: true } as NoDataResponse);
		} catch (err) {
			RootIPC.server.emit(socket, `${name}.response`, { text: `${err}`, error: true, final: true } as NoDataResponse);
		}
	});
	RootIPC.server.on(`${name}.mkdir`, async (data: { virtualPath: string, recursive?: boolean }, socket) => {
		try {
			datbox.mkdirSync(data.virtualPath, { recursive: data.recursive });
			RootIPC.server.emit(socket, `${name}.response`, { final: true } as NoDataResponse);
		} catch (err) {
			RootIPC.server.emit(socket, `${name}.response`, { text: `${err}`, error: true, final: true } as NoDataResponse);
		}
	});
	RootIPC.server.on(`${name}.copy`, async (data: { src: string, dest: string, recursive?: boolean }, socket) => {
		try {
			await datbox.cpAsync(data.src, data.dest, { recursive: data.recursive });
			RootIPC.server.emit(socket, `${name}.response`, { final: true } as NoDataResponse);
		} catch (err) {
			RootIPC.server.emit(socket, `${name}.response`, { text: `${err}`, error: true, final: true } as NoDataResponse);
		}
	});
});

RootIPC.server.start();