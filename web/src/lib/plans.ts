export interface PlanDef {
  id: string;
  name: string;
  price: string;
  interval: string;
}

export const PLANS: PlanDef[] = [
  { id: "starter", name: "Starter", price: "$19", interval: "mo" },
  { id: "team",    name: "Team",    price: "$99", interval: "mo" },
];
