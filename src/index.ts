import { program } from "commander";
import * as path from "path";
import { name, version } from "../package.json";
import envPaths from "env-paths";

const paths = envPaths(name, { suffix: "" });

program
	.name(name)
	.version(version);

const server = program.command("server")
	.description(`Run the ${name} local server`)
	.option("-c, --channel <id>", "ID of the text channel where chunks will be stored")
	.option("-C, --config <path>", `Local path to config file`, path.join(paths.config, "config.json"))
	.option("-r, --root <path>", "Root for the virtual file system")
	.option("-t, --token <token>", "Discord bot token. This option not recommended. Use .env or config instead")
	.action(() => { import("./server/server"); });

program.parse();

export { server };