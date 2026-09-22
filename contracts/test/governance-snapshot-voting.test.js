const { expect } = require("chai");
const { ethers } = require("hardhat");
const { deploySynWithBridge, mintSyn } = require("./helpers/syn");

describe("SYNTHOSGovernance snapshot voting", function () {
  async function deployFixture() {
    const [deployer, treasury, voterA, voterB, proposer] = await ethers.getSigners();
    // `deployer` is Hardhat's default signer, used implicitly by every
    // unconnected `Factory.deploy(...)` call below -- that makes it
    // owner() on SynCoin, which setGovernance requires.

    const { syn, minter } = await deploySynWithBridge(treasury, [treasury]);

    const Timelock = await ethers.getContractFactory("SYNTHOSTimelock");
    const timelock = await Timelock.deploy(
      60,
      [deployer.address],
      [ethers.ZeroAddress],
      deployer.address
    );
    await timelock.waitForDeployment();

    const Governance = await ethers.getContractFactory("SYNTHOSGovernance");
    const governance = await Governance.deploy(
      await syn.getAddress(),
      await timelock.getAddress()
    );
    await governance.waitForDeployment();

    await syn.connect(deployer).setGovernance(await governance.getAddress());

    // Fund `proposer` above PROPOSAL_THRESHOLD (100k SYN) so it can create
    // a proposal, and fund voterA with a real 1,000 SYN balance.
    await mintSyn(minter, [treasury], proposer.address, ethers.parseUnits("200000", 18), "proposer-funds");
    await mintSyn(minter, [treasury], voterA.address, ethers.parseUnits("1000", 18), "voterA-funds");

    return { deployer, treasury, voterA, voterB, proposer, syn, timelock, governance };
  }

  async function createProposal(governance, proposer) {
    const tx = await governance
      .connect(proposer)
      .createProposal(0, "Test proposal", "desc", [], [], [], []);
    const receipt = await tx.wait();
    const parsed = receipt.logs
      .map((log) => {
        try {
          return governance.interface.parseLog(log);
        } catch {
          return null;
        }
      })
      .find((entry) => entry && entry.name === "ProposalCreated");
    return parsed.args.proposal_id;
  }

  it("blocks a holder from voting again after moving the same tokens to a fresh wallet", async function () {
    const { voterA, voterB, proposer, syn, governance } = await deployFixture();

    const proposalId = await createProposal(governance, proposer);

    // voterA votes FOR with their real 1,000 SYN.
    await governance.connect(voterA).castVote(proposalId, 1);

    // voterA then moves the SAME 1,000 SYN to voterB, a second wallet they
    // also control, and voterB tries to vote too. Before this fix,
    // castVote read a live balanceOf(), so voterB's vote would count the
    // SAME 1,000 SYN a second time -- has_voted only ever stopped the same
    // ADDRESS voting twice, not the same underlying tokens voting through a
    // chain of addresses. After this fix, castVote reads the balance
    // snapshotted at proposal-creation time: voterB held ZERO SYN at that
    // moment (the transfer happens after), so voterB has no voting power
    // on this proposal at all.
    await syn.connect(voterA).transfer(voterB.address, ethers.parseUnits("1000", 18));
    await expect(governance.connect(voterB).castVote(proposalId, 1)).to.be.revertedWith(
      "No voting power"
    );

    const proposal = await governance.getProposal(proposalId);
    expect(proposal.votes_for).to.equal(ethers.parseUnits("1000", 18));
  });

  it("weighs a vote by the balance held at the proposal's snapshot, unaffected by transfers after it", async function () {
    const { voterA, proposer, syn, governance } = await deployFixture();

    const proposalId = await createProposal(governance, proposer);

    // voterA sends away most of their tokens AFTER the proposal (and its
    // snapshot) already exists.
    await syn.connect(voterA).transfer(proposer.address, ethers.parseUnits("900", 18));

    await governance.connect(voterA).castVote(proposalId, 1);

    const proposal = await governance.getProposal(proposalId);
    // voterA still votes with the FULL 1,000 SYN they held at the
    // snapshot, not the 100 SYN left in their wallet now -- vote weight is
    // fixed at proposal creation, not read live.
    expect(proposal.votes_for).to.equal(ethers.parseUnits("1000", 18));
  });

  it("gives a wallet that only acquires tokens after the snapshot zero voting power", async function () {
    const { voterA, voterB, proposer, syn, governance } = await deployFixture();

    const proposalId = await createProposal(governance, proposer);

    // voterB had nothing at snapshot time, then receives tokens afterward.
    await syn.connect(voterA).transfer(voterB.address, ethers.parseUnits("100", 18));

    await expect(governance.connect(voterB).castVote(proposalId, 1)).to.be.revertedWith(
      "No voting power"
    );
  });
});
