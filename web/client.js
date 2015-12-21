(function() {
    // $("#form").submit(function(e) {
    //
    // });
    var ws;
    var log = function(log) {
        return function(msg) {
            log.innerHTML += msg + "\n";
        }
    }(document.getElementById("log"));

    var send = function(msg) {
        if (ws) {
            var str = JSON.stringify(msg);
            log("send " + str);
            ws.send(str);
        }
    }

    var clist = document.getElementById("candidates");

    clist.onchange = function() {
        if (clist.selectedIndex >= 0) {
            var i = clist.selectedIndex;
            send({"Index": +clist.options[i].value});
        }
    };

    document.getElementById("disconnect").onclick = function() {
        if (ws != null) {
            log("disconnect")
            ws.close();
            ws = null;
        }
    }

    document.getElementById("join").onclick = function() {
        if (ws != null) {
            log("closing");
            ws.close();
        }

        clist.selectedIndex = -1;
        while (clist.firstChild)
            clist.removeChild(clist.firstChild);

        log("joining");
        ws = new WebSocket("ws://localhost:8080/pepasi")
        ws.onopen = function() {
            log("connected");
            var name = document.getElementById("name").value;
            var token = document.getElementById("token").value;
            log("joining as " + name + ", " + token);
            send({ID: token, Name: name, Timestamp: Date.now()});
        };
        ws.onmessage = function(e) {
            log("message: " + e.data);
            var msg = JSON.parse(e.data);
            if (msg.Name) {
                var opt = document.createElement("option");
                opt.appendChild(document.createTextNode("#" + msg.Index + " " + msg.Name));
                opt.value = msg.Index;
                clist.appendChild(opt);
            } else if (msg.Index != null) {
                log("registered as #" + msg.Index);
            }

            if (msg.Error){
                log("error: " + msg.Error);
            }
        };
        ws.onclose = function() {
            log("disconnected");
        };
        ws.onerror = function(e) {
            log("error: " + e);
        };
    }

    var movebtns = document.getElementById("moves").getElementsByTagName("button")
    for (var i = 0; i < movebtns.length; ++i) {
        (function (sym) {
            movebtns[i].onclick = function() {
                log("button: " + this.name);
                var token = document.getElementById("token").value;
                send({Timestamp: Date.now(), Symbol: sym});
            };
        })(i+1);
    }

}).call(this);
