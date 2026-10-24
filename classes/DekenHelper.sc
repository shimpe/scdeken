// Gets the Go helper `scdeken` onto this computer and runs jobs with it.
//
// Nothing here blocks the interpreter: the helper and curl run through
// unixCmd with an exit action, and the checksum is written to a file by the
// shell and read back afterwards.
//
// A job is a request file in, a result file out (`scdeken run`). The helper
// reports log lines and progress over OSC to the port in the request context.
// Responses are Events (ok, data, error, confirm, exit_code); `data` is the
// JSON as parsed by DekenJSON (Dictionaries with String keys).
DekenHelper {
	classvar <>lockPath;

	// ---- locating and downloading the helper

	// Calls onReady with the path of a usable helper, downloading and
	// verifying it first if needed. On failure it posts why and calls
	// onFail with the message.
	*ensure { |onReady, onFail|
		var lock, path;
		if(Deken.helperPath.notNil) { ^onReady.value(Deken.helperPath) };
		try {
			lock = this.prReadLock;
			path = this.installedPath(lock);
		} { |err|
			^this.prFail("could not read the helper description: " ++ err.errorString, onFail)
		};
		if(File.exists(path)) { ^onReady.value(path) };
		this.prDownload(lock, path, onReady, onFail)
	}

	// helper-lock.json next to classes/, unless lockPath says otherwise.
	*lockFile {
		^lockPath ?? { DekenHelper.filenameSymbol.asString.dirname.dirname +/+ "helper-lock.json" }
	}

	*prReadLock { ^DekenJSON.parse(File.readAllString(this.lockFile)) }

	*binDir { ^Platform.userAppSupportDir +/+ "sc-deken" +/+ "bin" }

	*exeExtension { ^if(DekenShell.isWindows) { ".exe" } { "" } }

	*installedPath { |lock|
		^this.binDir +/+ ("scdeken-" ++ lock["version"] ++ this.exeExtension)
	}

	// linux-x86_64 | linux-arm64 | macos-x86_64 | macos-arm64 | windows-x86_64
	*platformKey {
		var os = switch(thisProcess.platform.name, \windows, { "windows" }, \osx, { "macos" }, { "linux" });
		var arch = Platform.architecture.asString.toLower;
		arch = if(arch.find("arm").notNil or: { arch.find("aarch").notNil }) { "arm64" } { "x86_64" };
		^os ++ "-" ++ arch
	}

	*downloadURL { |lock|
		^lock["url"]
		.replace("{version}", lock["version"])
		.replace("{platform}", this.platformKey)
		.replace("{ext}", this.exeExtension)
	}

	*prDownload { |lock, path, onReady, onFail|
		var part = path ++ ".part";
		var expected = lock["sha256"] !? { |t| t[this.platformKey] };
		if(expected.isNil) {
			^this.prFail("this release lists no checksum for " ++ this.platformKey ++ ", so no helper can be downloaded.", onFail)
		};
		this.prMkdirs(this.binDir);
		this.prDiscard(part);
		this.prShell(this.curlCommand(part, this.downloadURL(lock))) { |exit|
			if(exit != 0) {
				this.prDiscard(part);
				this.prFail("downloading the helper failed (curl exit code %).".format(exit), onFail)
			} {
				this.prVerify(part, path, expected, onReady, onFail)
			}
		}
	}

	*curlCommand { |outFile, url|
		^DekenShell.command(if(DekenShell.isWindows) { "curl.exe" } { "curl" }, ["-fL", "-sS", "-o", outFile, url])
	}

	*prVerify { |part, path, expected, onReady, onFail|
		this.sha256(part) { |hash|
			if(hash.notNil and: { hash == expected.asString.toLower }) {
				this.prInstall(part, path, onReady, onFail)
			} {
				this.prDiscard(part);
				this.prFail("the downloaded helper did not match its checksum; not using it.", onFail)
			}
		}
	}

	// Calls action with the lowercase SHA-256 of file, or nil if it could not
	// be computed.
	*sha256 { |file, action|
		var out = file ++ ".sha";
		var cmd = this.hashCommand(file) ++ " > " ++ DekenShell.quote(out);
		this.prShell(cmd) { |exit|
			var text = if(File.exists(out)) { File.readAllString(out).stripWhiteSpace } { "" };
			this.prDiscard(out);
			action.value(if(exit == 0 and: { text.notEmpty }) { text.split($ )[0].toLower })
		}
	}

	*hashCommand { |file|
		^case
		{ DekenShell.isWindows } {
			DekenShell.command("powershell", ["-NoProfile", "-Command",
				"(Get-FileHash -Algorithm SHA256 -LiteralPath '%').Hash".format(file.replace("'", "''"))])
		}
		{ thisProcess.platform.name == \osx } { DekenShell.command("shasum", ["-a", "256", file]) }
		{ DekenShell.command("sha256sum", [file]) };
	}

	// chmod +x (Unix) and move into place.
	*prInstall { |part, path, onReady, onFail|
		var move = {
			var cmd = if(DekenShell.isWindows) { "move /Y " ++ DekenShell.quote(part) ++ " " ++ DekenShell.quote(path) } { DekenShell.command("mv", ["-f", part, path]) };
			this.prShell(cmd) { |exit|
				if(exit == 0 and: { File.exists(path) }) { onReady.value(path) } {
					this.prDiscard(part);
					this.prFail("could not move the helper into place at " ++ path, onFail)
				}
			}
		};
		if(DekenShell.isWindows) { move.value } {
			this.prShell(DekenShell.command("chmod", ["+x", part])) { |exit| move.value }
		}
	}

	// ---- running jobs

	// Runs one helper command. request is an Event such as
	// (command: "search", query: "fm"); the context and Deken.indexURL are
	// added here. onDone gets the response Event; onLog gets each log line;
	// onProgress gets (fraction, label).
	*run { |request, onDone, onLog, onProgress|
		var job = "j" ++ Date.getDate.rawSeconds.asInteger ++ UniqueID.next;
		this.ensure({ |helper| this.prLaunch(helper, job, request, onDone, onLog, onProgress) }, { |msg|
			onDone.value((ok: false, error: msg, exit_code: -1))
		})
	}

	*jobsDir { ^Platform.userAppSupportDir +/+ "sc-deken" +/+ "jobs" }

	*prLaunch { |helper, job, request, onDone, onLog, onProgress|
		var req = this.jobsDir +/+ (job ++ ".request.json");
		var res = this.jobsDir +/+ (job ++ ".result.json");
		this.prMkdirs(this.jobsDir);
		this.prWriteRequest(req, job, request);
		this.prListen(job, onLog, onProgress);
		this.prShell(DekenShell.command(helper, ["run", req, res])) { |exit|
			var response = this.prReadResponse(res, exit);
			// OSC is UDP: give the last log lines a moment to arrive.
			AppClock.sched(0.2, {
				this.prUnlisten(job);
				this.prDiscard(req);
				this.prDiscard(res);
				onDone.value(response);
				nil
			})
		}
	}

	*prWriteRequest { |path, job, request|
		var body = request.copy;
		var file;
		body[\context] = DekenContext.build(job);
		if(Deken.indexURL.notNil) { body[\index_url] = Deken.indexURL };
		file = File(path, "w");
		protect { file.write(DekenJSON.stringify(body)) } { file.close };
	}

	*prListen { |job, onLog, onProgress|
		OSCdef(this.prKey(\log, job), { |msg|
			if(msg[1].asString == job) { onLog.value(msg[2].asString) }
		}, '/deken/log');
		OSCdef(this.prKey(\progress, job), { |msg|
			if(msg[1].asString == job) { onProgress.value(msg[2], msg[3].asString) }
		}, '/deken/progress');
	}

	*prUnlisten { |job|
		OSCdef(this.prKey(\log, job)).free;
		OSCdef(this.prKey(\progress, job)).free;
	}

	*prKey { |kind, job| ^("deken_" ++ kind ++ "_" ++ job).asSymbol }

	*prReadResponse { |path, exit|
		var doc;
		if(File.exists(path).not) {
			^(ok: false, error: "scdeken did not answer (exit code %).".format(exit), exit_code: exit)
		};
		doc = try { DekenJSON.parse(File.readAllString(path)) };
		if(doc.isNil) {
			^(ok: false, error: "scdeken's answer could not be read.", exit_code: exit)
		};
		^(ok: doc["ok"] == true, data: doc["data"], error: doc["error"], confirm: doc["confirm"] ? [], exit_code: exit)
	}

	// ---- small shared pieces

	*prShell { |command, action| command.unixCmd(action, false) }

	*prDiscard { |file| if(File.exists(file)) { File.delete(file) } }

	*prFail { |msg, onFail|
		("sc-deken: " ++ msg).postln;
		onFail.value(msg)
	}

	// File.mkdir makes one level only.
	*prMkdirs { |dir|
		if(File.exists(dir).not) {
			this.prMkdirs(dir.dirname);
			File.mkdir(dir)
		}
	}
}
