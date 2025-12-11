import RootIPC from "node-ipc";
import { remove } from "..";
import { randomUUID } from "crypto";
import { name } from "../../package.json";
import type { NoDataResponse } from "../types"

const virtualPath = remove.args[0]!;
const { recursive, remote } = remove.opts<{ recursive?: boolean, remote?: boolean }>();


RootIPC.config.id = randomUUID();
RootIPC.connectTo(name, () => {
	const of = RootIPC.of[name]!;
	of.on("connect", () => {
		of.emit(`${name}.remove`, { virtualPath, recursive, remote });
	});
	
	of.on(`${name}.response`, (data: NoDataResponse) => {
		if (data.text) {
			if (data.error) console.error(data.text);
			else console.log(data.text);
		}
		if (data.final) {
			RootIPC.disconnect(name);
		}
	});
});