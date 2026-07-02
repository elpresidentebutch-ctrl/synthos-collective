// SPDX-License-Identifier: Apache-2.0
pragma solidity ^0.8.20;

import "@openzeppelin/contracts/access/Ownable.sol";

/**
 * @title SYNTHOSComplianceRegistry
 * @dev Compliance-by-design registry for SYNTHOS token distributions.
 *
 * This contract does not decide legal status and does not guarantee compliance.
 * It creates an auditable on-chain control layer that distribution contracts,
 * reward contracts, treasury contracts, and launch scripts can check before
 * releasing SYN to a recipient.
 */
contract SYNTHOSComplianceRegistry is Ownable {
    enum RecipientCategory {
        None,
        Founder,
        CMO,
        ImmuneOperator,
        Validator,
        Treasury,
        Community,
        Liquidity,
        StrategicReserve,
        Ecosystem
    }

    struct ComplianceRecord {
        RecipientCategory category;
        bool walletVerified;
        bool jurisdictionEligible;
        bool disclosureAcknowledged;
        bool revoked;
        uint64 lockupUntil;
        bytes32 disclosureHash;
        bytes32 jurisdictionHash;
        uint64 updatedAt;
    }

    mapping(address => ComplianceRecord) private records;
    bool public communitySelfRegistrationOpen;

    event ComplianceRecordUpdated(
        address indexed account,
        RecipientCategory indexed category,
        bool walletVerified,
        bool jurisdictionEligible,
        bool disclosureAcknowledged,
        bool revoked,
        uint64 lockupUntil,
        bytes32 disclosureHash,
        bytes32 jurisdictionHash
    );
    event DisclosureAcknowledged(address indexed account, bytes32 indexed disclosureHash);
    event RecipientRevoked(address indexed account, string reason);
    event RecipientRestored(address indexed account);
    event CommunitySelfRegistrationOpenUpdated(bool open);

    function setCommunitySelfRegistrationOpen(bool open) external onlyOwner {
        communitySelfRegistrationOpen = open;
        emit CommunitySelfRegistrationOpenUpdated(open);
    }

    function selfRegisterCommunity(
        bytes32 disclosureHash,
        bytes32 jurisdictionHash
    ) external {
        require(communitySelfRegistrationOpen, "community registration closed");
        require(disclosureHash != bytes32(0), "disclosure hash required");
        require(jurisdictionHash != bytes32(0), "jurisdiction hash required");

        records[msg.sender] = ComplianceRecord({
            category: RecipientCategory.Community,
            walletVerified: true,
            jurisdictionEligible: true,
            disclosureAcknowledged: true,
            revoked: false,
            lockupUntil: 0,
            disclosureHash: disclosureHash,
            jurisdictionHash: jurisdictionHash,
            updatedAt: uint64(block.timestamp)
        });

        emit DisclosureAcknowledged(msg.sender, disclosureHash);
        emit ComplianceRecordUpdated(
            msg.sender,
            RecipientCategory.Community,
            true,
            true,
            true,
            false,
            0,
            disclosureHash,
            jurisdictionHash
        );
    }

    function setComplianceRecord(
        address account,
        RecipientCategory category,
        bool walletVerified,
        bool jurisdictionEligible,
        bool disclosureAcknowledged,
        bool revoked,
        uint64 lockupUntil,
        bytes32 disclosureHash,
        bytes32 jurisdictionHash
    ) external onlyOwner {
        require(account != address(0), "invalid account");
        require(category != RecipientCategory.None, "category required");

        records[account] = ComplianceRecord({
            category: category,
            walletVerified: walletVerified,
            jurisdictionEligible: jurisdictionEligible,
            disclosureAcknowledged: disclosureAcknowledged,
            revoked: revoked,
            lockupUntil: lockupUntil,
            disclosureHash: disclosureHash,
            jurisdictionHash: jurisdictionHash,
            updatedAt: uint64(block.timestamp)
        });

        emit ComplianceRecordUpdated(
            account,
            category,
            walletVerified,
            jurisdictionEligible,
            disclosureAcknowledged,
            revoked,
            lockupUntil,
            disclosureHash,
            jurisdictionHash
        );
    }

    function acknowledgeDisclosure(bytes32 disclosureHash) external {
        require(disclosureHash != bytes32(0), "disclosure hash required");

        ComplianceRecord storage record = records[msg.sender];
        require(record.category != RecipientCategory.None, "record missing");
        require(!record.revoked, "recipient revoked");

        record.disclosureHash = disclosureHash;
        record.disclosureAcknowledged = true;
        record.updatedAt = uint64(block.timestamp);

        emit DisclosureAcknowledged(msg.sender, disclosureHash);
        emit ComplianceRecordUpdated(
            msg.sender,
            record.category,
            record.walletVerified,
            record.jurisdictionEligible,
            record.disclosureAcknowledged,
            record.revoked,
            record.lockupUntil,
            record.disclosureHash,
            record.jurisdictionHash
        );
    }

    function revokeRecipient(address account, string calldata reason) external onlyOwner {
        require(account != address(0), "invalid account");
        ComplianceRecord storage record = records[account];
        require(record.category != RecipientCategory.None, "record missing");

        record.revoked = true;
        record.updatedAt = uint64(block.timestamp);

        emit RecipientRevoked(account, reason);
        emit ComplianceRecordUpdated(
            account,
            record.category,
            record.walletVerified,
            record.jurisdictionEligible,
            record.disclosureAcknowledged,
            record.revoked,
            record.lockupUntil,
            record.disclosureHash,
            record.jurisdictionHash
        );
    }

    function restoreRecipient(address account) external onlyOwner {
        require(account != address(0), "invalid account");
        ComplianceRecord storage record = records[account];
        require(record.category != RecipientCategory.None, "record missing");

        record.revoked = false;
        record.updatedAt = uint64(block.timestamp);

        emit RecipientRestored(account);
        emit ComplianceRecordUpdated(
            account,
            record.category,
            record.walletVerified,
            record.jurisdictionEligible,
            record.disclosureAcknowledged,
            record.revoked,
            record.lockupUntil,
            record.disclosureHash,
            record.jurisdictionHash
        );
    }

    function getComplianceRecord(address account) external view returns (ComplianceRecord memory) {
        return records[account];
    }

    function eligibleToReceive(
        address account,
        RecipientCategory expectedCategory
    ) public view returns (bool) {
        ComplianceRecord memory record = records[account];
        if (account == address(0)) return false;
        if (record.category == RecipientCategory.None) return false;
        if (record.category != expectedCategory) return false;
        if (!record.walletVerified) return false;
        if (!record.jurisdictionEligible) return false;
        if (!record.disclosureAcknowledged) return false;
        if (record.revoked) return false;
        if (record.lockupUntil > block.timestamp) return false;
        return true;
    }

    function requireEligible(
        address account,
        RecipientCategory expectedCategory
    ) external view {
        require(eligibleToReceive(account, expectedCategory), "recipient not eligible");
    }
}
