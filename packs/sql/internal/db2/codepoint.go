package db2

// The code points of DDM, the layer of DRDA that names every command, reply
// and parameter by a number (DRDA, Version 5, Volume 3: Distributed Data
// Management Architecture, The Open Group).
const (
	// Commands.
	cpEXCSAT    = 0x1041 // exchange server attributes
	cpACCSEC    = 0x106D // access security
	cpSECCHK    = 0x106E // security check
	cpACCRDB    = 0x2001 // access a database
	cpINTRDBRQS = 0x2003 // interrupt a database's request
	cpCNTQRY    = 0x2006 // continue a query
	cpEXCSQLIMM = 0x200A // execute a statement immediately
	cpOPNQRY    = 0x200C // open a query
	cpPRPSQLSTT = 0x200D // prepare a statement
	cpRDBCMM    = 0x200E // commit
	cpRDBRLLBCK = 0x200F // roll back

	// Objects a command carries, and objects of a reply.
	cpSQLSTT  = 0x2414 // a statement's text
	cpSQLCARD = 0x2408 // how a statement ended
	cpSQLDARD = 0x2411 // a statement's columns
	cpQRYDSC  = 0x241A // how a query's rows are laid out
	cpQRYDTA  = 0x241B // a query's rows
	cpEXTDTA  = 0x146C // a value sent outside its row

	// Replies.
	cpEXCSATRD = 0x1443
	cpACCSECRD = 0x14AC
	cpSECCHKRM = 0x1219
	cpACCRDBRM = 0x2201
	cpOPNQRYRM = 0x2205
	cpENDQRYRM = 0x220B
	cpENDUOWRM = 0x220C
	cpQRYNOPRM = 0x2202
	cpQRYPOPRM = 0x220F
	cpRDBUPDRM = 0x2218
	cpSQLERRRM = 0x2213
	cpOPNQFLRM = 0x2212
	cpRDBNFNRM = 0x2211
	cpRDBNACRM = 0x2204
	cpRDBATHRM = 0x22CB
	cpRDBAFLRM = 0x221A
	cpRDBACCRM = 0x2207
	cpABNUOWRM = 0x220D
	cpAGNPRMRM = 0x1232
	cpCMDCHKRM = 0x1254
	cpCMDNSPRM = 0x1250
	cpCMDATHRM = 0x121C
	cpCMDVLTRM = 0x221D
	cpDSCINVRM = 0x220A
	cpDTAMCHRM = 0x220E
	cpMGRLVLRM = 0x1210
	cpOBJNSPRM = 0x1253
	cpPRCCNVRM = 0x1245
	cpPRMNSPRM = 0x1251
	cpRSCLMTRM = 0x1233
	cpSYNTAXRM = 0x124C
	cpTRGNSPRM = 0x125F
	cpVALNSPRM = 0x1252
	cpPKGBNARM = 0x2206
	cpCMMRQSRM = 0x2225
	cpCMDCMPRM = 0x124B // the command is complete

	// Parameters.
	cpEXTNAM    = 0x115E // the client's name
	cpSRVNAM    = 0x116D // the machine's name
	cpSRVRLSLV  = 0x115A // the product's release
	cpSRVCLSNM  = 0x1147 // the product's class
	cpMGRLVLLS  = 0x1404 // the managers and their levels
	cpSECMEC    = 0x11A2 // how a user is proved
	cpSECCHKCD  = 0x11A4 // why a user was refused
	cpSECTKN    = 0x11DC // a token of the proof
	cpENCALG    = 0x1909 // the cipher of an encrypted proof
	cpUSRID     = 0x11A0
	cpPASSWORD  = 0x11A1
	cpRDBNAM    = 0x2110 // the database's name
	cpRDBACCCL  = 0x210F // the access method: SQLAM
	cpPRDID     = 0x112E // the product
	cpTYPDEFNAM = 0x002F // how numbers are laid out
	cpTYPDEFOVR = 0x0035 // the character sets
	cpCCSIDSBC  = 0x119C
	cpCCSIDDBC  = 0x119D
	cpCCSIDMBC  = 0x119E
	cpRDBINTTKN = 0x2103 // the token a request is interrupted by
	cpPKGNAMCSN = 0x2113 // a package's section
	cpRDBCMTOK  = 0x2105 // a commit is allowed
	cpRTNSQLDA  = 0x2116 // return the columns
	cpTYPSQLDA  = 0x2146 // which description of them
	cpQRYBLKSZ  = 0x2114 // the size of a block of rows
	cpMAXBLKEXT = 0x2141 // how many more blocks a row may take
	cpQRYCLSIMP = 0x215D // close a query at its end
	cpQRYINSID  = 0x215B // which opening of a query
	cpRTNEXTDTA = 0x2148 // when values outside rows are sent
	cpSRVDGN    = 0x1153 // the server's diagnosis
	cpSVRCOD    = 0x1149 // how severe
	cpSYNERRCD  = 0x114A // which rule of syntax
	cpCODPNT    = 0x000C // the code point at fault
	cpPRCCNVCD  = 0x113F

	// Managers.
	mgrAGENT      = 0x1403
	mgrSQLAM      = 0x2407
	mgrRDB        = 0x240F
	mgrSECMGR     = 0x1440
	mgrCMNTCPIP   = 0x1474
	mgrCCSIDMGR   = 0x14CC
	mgrUNICODEMGR = 0x1C08

	// Ways to prove a user.
	secUserPassword          = 3 // a user and a password, as they are
	secEncryptedPassword     = 7 // a user, and a password encrypted
	secEncryptedUserPassword = 9 // both encrypted

	// Why a user was refused (SECCHKCD).
	secOK              = 0x00
	secMechUnsupported = 0x01
	secPasswordExpired = 0x0E
	secPasswordInvalid = 0x0F
	secPasswordMissing = 0x10
	secUserMissing     = 0x12
	secUserInvalid     = 0x13
	secUserRevoked     = 0x14
	secNewPassword     = 0x15
	secLocalFailed     = 0x0A
)
