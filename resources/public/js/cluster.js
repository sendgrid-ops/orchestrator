
function generateInstanceDivs(nodesList) {
    var nodesMap = nodesList.reduce(function (map, node) {
        map[node.id] = node;
        return map;
    }, {});

    $("[data-fo-id]").each(function () {
        var id = $(this).attr("data-fo-id");
        $(this).html(
        	'<div xmlns="http://www.w3.org/1999/xhtml" class="popover right instance"><div class="arrow"></div><h3 class="popover-title"></h3><div class="popover-content"></div></div>'
        );
    });
    nodesList.forEach(function (node) {
    	var popoverElement = $("[data-fo-id='" + node.id + "'] .popover");
    	renderInstanceElement(popoverElement, node, "cluster");
    });
    $("[data-fo-id]").each(
        function () {
            var id = $(this).attr("data-fo-id");
            var popoverDiv = $("[data-fo-id='" + id + "'] div.popover");

            popoverDiv.attr("x", $(this).attr("x"));
            $(this).attr("y",
                0 - popoverDiv.height() / 2 - 2);
            popoverDiv.attr("y", $(this).attr("y"));
            $(this).attr("width",
                popoverDiv.width() + 30);
            $(this).attr("height",
                popoverDiv.height() +16);
        });
    $("div.popover").popover();
    $("div.popover").show();
    
    $("[data-fo-id]").on("mouseenter", ".popover[data-nodeid]", function() {
    	if ($(".popover.instance[data-duplicate-node]").hasClass("ui-draggable-dragging")) {
    		// Do not remove & recreate while dragging. Ignore any mouseenter
    		return false;
    	}
    	var draggedNodeId = $(this).attr("data-nodeid"); 
    	if (draggedNodeId == $(".popover.instance[data-duplicate-node]").attr("data-nodeid")) {
    		return false;
    	}
    	$(".popover.instance[data-duplicate-node]").remove();
    	var duplicate = $(this).clone().appendTo("#cluster_container");
    	$(duplicate).attr("data-duplicate-node", "true");
    	//$(".popover.instance[data-duplicate-node] h3").addClass("label-primary");
    	$(duplicate).css({"margin-left": "0"});
    	$(duplicate).css($(this).offset());
    	$(duplicate).width($(this).width());
    	$(duplicate).height($(this).height());
    	$(duplicate).popover();
        $(duplicate).show();
        $(".popover.instance[data-duplicate-node] h3 a").click(function () {
        	openNodeModal(nodesMap[draggedNodeId]);
        	return false;
        });
        $(duplicate).draggable({
        	addClasses: true, 
        	opacity: 0.67,
        	cancel: "#cluster_container .popover.instance h3 a",
        	start: function(event, ui) {
        		resetRefreshTimer();
        		$("#cluster_container .accept_drop").removeClass("accept_drop");
        		$("#cluster_container .popover.instance").droppable({
        			accept: function(draggable) {
        				var draggedNode = nodesMap[draggedNodeId];
        				var targetNode = nodesMap[$(this).attr("data-nodeid")];
        				var acceptDrop =  moveInstance(draggedNode, targetNode, false);
        				if (acceptDrop) {
        					$(this).addClass("accept_drop");
        				}
        				return acceptDrop;
        			},
        			hoverClass: "draggable-hovers",
					drop: function( event, ui ) {
				        $(".popover.instance[data-duplicate-node]").remove();
				        moveInstance(nodesMap[draggedNodeId], nodesMap[$(this).attr("data-nodeid")], true);
					}
        		});
        	},
	    	drag: function(event, ui) {
	    		resetRefreshTimer();
	    	},
	    	stop: function(event, ui) {
	    		resetRefreshTimer();
        		$("#cluster_container .accept_drop").removeClass("accept_drop");
	    	}
        });
    	$(duplicate).on("mouseleave", function() {
    		if (!$(this).hasClass("ui-draggable-dragging")) {
	    		$(this).remove();
    		}
    	});
    	// Don't ask why the following... jqueryUI recognizes the click as start drag, but fails to stop...
    	$(duplicate).on("click", function() {
        	$("#cluster_container .accept_drop").removeClass("accept_drop");
        	return false;
        });	
    });
}

$(document)
    .ready(
        function () {
            $.get("/api/cluster/"+currentClusterName(), function (instances) {
                $.get("/api/maintenance",
                    function (maintenanceList) {
                		var instancesMap = normalizeInstances(instances, maintenanceList);
                        visualizeInstances(instances, instancesMap);
                        generateInstanceDivs(instances);
                    }, "json");
            }, "json");
            
            startRefreshTimer();
            $(document).click(function() {
            	resetRefreshTimer();
            });
            $(document).mousemove(function() {
            	resetRefreshTimer();
            });
        });
