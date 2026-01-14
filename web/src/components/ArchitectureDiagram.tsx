import { useCallback, useMemo, useEffect } from 'react';
import {
  ReactFlow,
  Controls,
  Background,
  MiniMap,
  useNodesState,
  useEdgesState,
  MarkerType,
  BackgroundVariant,
  type NodeMouseHandler,
  type Node,
} from '@xyflow/react';
import '@xyflow/react/dist/style.css';

import { ResourceNode } from './diagram/ResourceNode';
import { CategoryLane } from './diagram/CategoryLane';
import type { Resource } from '../lib/migrate-api';
import type { ResourceNode as ResourceNodeType, DependencyEdge } from '../lib/diagram-types';
import { categoryColors, categoryLabels } from '../lib/diagram-types';

interface ArchitectureDiagramProps {
  resources: Resource[];
  selected: Set<string>;
  onToggle: (id: string) => void;
  onSelect?: (resource: Resource) => void;
}

const nodeTypes = {
  resource: ResourceNode,
  categoryLane: CategoryLane,
};

// Layout constants
const NODE_WIDTH = 200;
const NODE_HEIGHT = 90;
const NODE_GAP_X = 30;
const NODE_GAP_Y = 25;
const LANE_PADDING_X = 30;
const LANE_PADDING_Y = 40; // Extra space for floating header
const LANE_HEADER_HEIGHT = 20; // Reduced since header floats above
const LANE_GAP_Y = 60; // Vertical gap between lanes
const MAX_COLS_PER_LANE = 5; // Max nodes per row within a category

// Logical 2D grid layout: row and column position for each category type
// Row 0: Entry points (networking, security)
// Row 1: Compute layer (compute, serverless)
// Row 2: Data layer (storage, database, messaging)
const CATEGORY_GRID: Record<string, { row: number; col: number }> = {
  networking: { row: 0, col: 0 },
  security: { row: 0, col: 1 },
  compute: { row: 1, col: 0 },
  serverless: { row: 1, col: 1 },
  storage: { row: 2, col: 0 },
  object_storage: { row: 2, col: 1 },
  database: { row: 2, col: 2 },
  sql_database: { row: 2, col: 2 },
  messaging: { row: 1, col: 2 },
  queue: { row: 1, col: 2 },
};

function getCategoryGridPosition(category: string): { row: number; col: number } {
  const lowerCat = category.toLowerCase();

  // Check for keyword matches
  if (lowerCat.includes('network') || lowerCat.includes('vpc') || lowerCat.includes('route') || lowerCat.includes('dns') || lowerCat.includes('cdn') || lowerCat.includes('cloudfront') || lowerCat.includes('apigateway') || lowerCat.includes('alb') || lowerCat.includes('elb'))
    return { row: 0, col: 0 };
  if (lowerCat.includes('security') || lowerCat.includes('iam') || lowerCat.includes('cognito') || lowerCat.includes('firewall') || lowerCat.includes('waf'))
    return { row: 0, col: 1 };
  if (lowerCat.includes('serverless') || lowerCat.includes('lambda') || lowerCat.includes('function'))
    return { row: 1, col: 1 };
  if (lowerCat.includes('compute') || lowerCat.includes('ec2') || lowerCat.includes('ecs') || lowerCat.includes('container'))
    return { row: 1, col: 0 };
  if (lowerCat.includes('messag') || lowerCat.includes('queue') || lowerCat.includes('sqs') || lowerCat.includes('sns') || lowerCat.includes('event'))
    return { row: 1, col: 2 };
  if (lowerCat.includes('object') || lowerCat.includes('s3') || lowerCat.includes('bucket') || lowerCat.includes('blob'))
    return { row: 2, col: 1 };
  if (lowerCat.includes('storage'))
    return { row: 2, col: 0 };
  if (lowerCat.includes('database') || lowerCat.includes('sql') || lowerCat.includes('rds') || lowerCat.includes('dynamo'))
    return { row: 2, col: 2 };

  return CATEGORY_GRID[category] || { row: 3, col: 1 }; // Default to bottom center
}

// Grid gap between category lanes
const LANE_GAP_X = 50;

// 2D Grid-based layout: categories positioned logically
function getLayoutedElements(
  resources: Resource[],
  selected: Set<string>,
  warnings: Set<string>
): { nodes: Node[]; edges: DependencyEdge[] } {
  // Group resources by category
  const byCategory = new Map<string, Resource[]>();
  for (const res of resources) {
    const cat = res.category || 'other';
    if (!byCategory.has(cat)) {
      byCategory.set(cat, []);
    }
    byCategory.get(cat)!.push(res);
  }

  // Calculate lane dimensions for each category first
  const laneDimensions = new Map<string, { width: number; height: number; items: Resource[] }>();
  for (const [category, items] of byCategory.entries()) {
    const numCols = Math.min(items.length, MAX_COLS_PER_LANE);
    const numRows = Math.ceil(items.length / numCols);
    const laneContentWidth = numCols * NODE_WIDTH + (numCols - 1) * NODE_GAP_X;
    const laneContentHeight = numRows * NODE_HEIGHT + (numRows - 1) * NODE_GAP_Y;
    laneDimensions.set(category, {
      width: laneContentWidth + LANE_PADDING_X * 2,
      height: laneContentHeight + LANE_PADDING_Y * 2 + LANE_HEADER_HEIGHT,
      items,
    });
  }

  // Group categories by grid position
  const gridCells = new Map<string, string[]>(); // "row,col" -> categories
  for (const category of byCategory.keys()) {
    const pos = getCategoryGridPosition(category);
    const key = `${pos.row},${pos.col}`;
    if (!gridCells.has(key)) {
      gridCells.set(key, []);
    }
    gridCells.get(key)!.push(category);
  }

  // Calculate max width per column and max height per row
  const colWidths = new Map<number, number>();
  const rowHeights = new Map<number, number>();

  for (const [key, categories] of gridCells.entries()) {
    const [row, col] = key.split(',').map(Number);
    let cellHeight = 0;
    let cellWidth = 0;

    for (const cat of categories) {
      const dim = laneDimensions.get(cat)!;
      cellWidth = Math.max(cellWidth, dim.width);
      cellHeight += dim.height + (cellHeight > 0 ? 20 : 0); // Stack categories in same cell
    }

    colWidths.set(col, Math.max(colWidths.get(col) || 0, cellWidth));
    rowHeights.set(row, Math.max(rowHeights.get(row) || 0, cellHeight));
  }

  // Calculate column X positions
  const colX = new Map<number, number>();
  let currentX = 0;
  const maxCol = Math.max(...Array.from(colWidths.keys()), 0);
  for (let c = 0; c <= maxCol; c++) {
    colX.set(c, currentX);
    currentX += (colWidths.get(c) || 300) + LANE_GAP_X;
  }

  // Calculate row Y positions
  const rowY = new Map<number, number>();
  let currentY = 0;
  const maxRow = Math.max(...Array.from(rowHeights.keys()), 0);
  for (let r = 0; r <= maxRow; r++) {
    rowY.set(r, currentY);
    currentY += (rowHeights.get(r) || 200) + LANE_GAP_Y;
  }

  // Position lanes and nodes
  const resourceNodes: ResourceNodeType[] = [];
  const laneNodes: Node[] = [];

  for (const [key, categories] of gridCells.entries()) {
    const [row, col] = key.split(',').map(Number);
    const baseX = colX.get(col) || 0;
    let laneY = rowY.get(row) || 0;

    for (const category of categories) {
      const dim = laneDimensions.get(category)!;
      const items = dim.items;
      const numCols = Math.min(items.length, MAX_COLS_PER_LANE);

      // Position nodes within the lane
      items.forEach((res, i) => {
        const nodeCol = i % numCols;
        const nodeRow = Math.floor(i / numCols);

        resourceNodes.push({
          id: res.id,
          type: 'resource',
          position: {
            x: baseX + LANE_PADDING_X + nodeCol * (NODE_WIDTH + NODE_GAP_X),
            y: laneY + LANE_HEADER_HEIGHT + LANE_PADDING_Y + nodeRow * (NODE_HEIGHT + NODE_GAP_Y),
          },
          data: {
            resource: res,
            selected: selected.has(res.id),
            hasWarning: warnings.has(res.id),
          },
        });
      });

      // Create lane background
      laneNodes.push({
        id: `lane-${category}`,
        type: 'categoryLane',
        position: { x: baseX, y: laneY },
        data: {
          category,
          label: categoryLabels[category] || category.replace(/_/g, ' ').toUpperCase(),
          color: getCategoryColor(category),
          width: dim.width,
          height: dim.height,
        },
        draggable: false,
        selectable: false,
        zIndex: -1,
      });

      laneY += dim.height + 20; // Stack multiple categories in same cell
    }
  }

  // Create edges
  const edges: DependencyEdge[] = [];
  const nodePositions = new Map(resourceNodes.map(n => [n.id, n.position]));

  for (const res of resources) {
    for (const depId of res.dependencies || []) {
      if (nodePositions.has(depId)) {
        const isActive = selected.has(res.id) && selected.has(depId);
        const color = getCategoryColor(res.category);

        edges.push({
          id: `${res.id}-${depId}`,
          source: depId,
          target: res.id,
          type: 'smoothstep',
          animated: isActive,
          markerEnd: {
            type: MarkerType.ArrowClosed,
            color: isActive ? color : '#d1d5db',
            width: 20,
            height: 20,
          },
          style: {
            stroke: isActive ? color : '#d1d5db',
            strokeWidth: isActive ? 2 : 1,
            opacity: isActive ? 1 : 0.3,
          },
        });
      }
    }
  }

  return {
    nodes: [...laneNodes, ...resourceNodes],
    edges,
  };
}

function getCategoryColor(category: string): string {
  const lowerCat = category.toLowerCase();

  if (lowerCat.includes('network')) return categoryColors.networking || '#f59e0b';
  if (lowerCat.includes('security') || lowerCat.includes('iam')) return categoryColors.security || '#ef4444';
  if (lowerCat.includes('compute') || lowerCat.includes('serverless') || lowerCat.includes('lambda') || lowerCat.includes('function')) return categoryColors.compute || '#3b82f6';
  if (lowerCat.includes('storage') || lowerCat.includes('s3') || lowerCat.includes('bucket') || lowerCat.includes('object')) return categoryColors.storage || '#8b5cf6';
  if (lowerCat.includes('database') || lowerCat.includes('sql') || lowerCat.includes('rds') || lowerCat.includes('dynamo')) return categoryColors.database || '#10b981';
  if (lowerCat.includes('messag') || lowerCat.includes('queue') || lowerCat.includes('sqs') || lowerCat.includes('sns') || lowerCat.includes('event')) return categoryColors.messaging || '#06b6d4';

  return categoryColors[category] || '#6b7280';
}

export function ArchitectureDiagram({ resources, selected, onToggle, onSelect }: ArchitectureDiagramProps) {
  // Calculate which resources have broken dependencies
  const warnings = useMemo(() => {
    const warningSet = new Set<string>();
    for (const res of resources) {
      if (!selected.has(res.id)) continue;
      for (const depId of res.dependencies || []) {
        if (!selected.has(depId)) {
          warningSet.add(res.id);
          break;
        }
      }
    }
    return warningSet;
  }, [resources, selected]);

  // Get layouted nodes and edges
  const { nodes: initialNodes, edges: initialEdges } = useMemo(
    () => getLayoutedElements(resources, selected, warnings),
    [resources, selected, warnings]
  );

  const [nodes, setNodes, onNodesChange] = useNodesState(initialNodes);
  const [edges, setEdges, onEdgesChange] = useEdgesState(initialEdges);

  // Update when selection changes
  useEffect(() => {
    setNodes(initialNodes);
    setEdges(initialEdges);
  }, [initialNodes, initialEdges, setNodes, setEdges]);

  const handleNodeClick: NodeMouseHandler = useCallback((_, node) => {
    if (node.type === 'resource') {
      onToggle(node.id);
      // Also call onSelect with the full resource
      if (onSelect) {
        const data = node.data as { resource: Resource };
        onSelect(data.resource);
      }
    }
  }, [onToggle, onSelect]);

  // MiniMap node color
  const nodeColor = useCallback((node: Node) => {
    if (node.type === 'categoryLane') {
      return (node.data as { color: string }).color + '30';
    }
    const data = node.data as { resource: Resource; selected: boolean };
    if (!data.selected) return '#d1d5db';
    return getCategoryColor(data.resource?.category || '');
  }, []);

  return (
    <div className="w-full h-full bg-gradient-to-br from-slate-50 to-slate-100">
      <ReactFlow
        nodes={nodes}
        edges={edges}
        onNodesChange={onNodesChange}
        onEdgesChange={onEdgesChange}
        onNodeClick={handleNodeClick}
        nodeTypes={nodeTypes}
        fitView
        fitViewOptions={{ padding: 0.1, maxZoom: 1 }}
        minZoom={0.15}
        maxZoom={2}
        proOptions={{ hideAttribution: true }}
        defaultEdgeOptions={{
          type: 'smoothstep',
        }}
      >
        <Background
          variant={BackgroundVariant.Dots}
          color="#cbd5e1"
          gap={20}
          size={1}
        />
        <Controls
          showInteractive={false}
          className="bg-white shadow-md rounded-lg border border-slate-200"
        />
        <MiniMap
          nodeColor={nodeColor}
          maskColor="rgba(255, 255, 255, 0.8)"
          className="bg-white shadow-md rounded-lg border border-slate-200"
          pannable
          zoomable
        />
      </ReactFlow>
    </div>
  );
}
