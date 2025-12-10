import RootIPC from "node-ipc";
import * as path from "path";
import { upload } from "..";
import { name } from "../../package.json";
import { randomUUID } from "crypto";
import type { TransferResponse } from "../types";

const localPath = path.resolve(upload.args[0]!);
const virtualPath = upload.args[1];

RootIPC.config.id = randomUUID();
RootIPC.connectTo(name, () => {
	const of = RootIPC.of[name]!;
	of.on("connect", () => {
		of.emit(`${name}.upload`, { localPath, virtualPath });
	});
	
	of.on(`${name}.response`, (data: TransferResponse) => {
		if (data.text) {
			process.stdout.write("\n");
			if (data.error) console.error(data.text);
			else console.log(data.text);
		}
		if (data.data) {
			const { current, total } = data.data;
			const percentage = Math.round(100 * current / total).toString().padStart(3, "0");
			if (process.stdout.columns < 12) console.log(`${percentage}%`);
			else {
				const barLength = process.stdout.columns - 7;
				const progressBar = Array(barLength)
					.fill(0)
					.map((_, ii) => (current / total > ii / barLength) ? "#" : " ");
				process.stdout.write(`\r${percentage}% [${progressBar.join("")}]`);
			}
		}
		if (data.final) {
			RootIPC.disconnect(name);
		}
	});
});